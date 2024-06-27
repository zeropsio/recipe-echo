package main

import (
	"context"
	"database/sql"
	"embed"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/sessions"
	"github.com/rbcervilla/redisstore/v8"

	"github.com/go-redis/redis/v8"

	_ "github.com/joho/godotenv/autoload"
	"github.com/labstack/echo-contrib/session"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/labstack/gommon/log"
	_ "github.com/lib/pq"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/wneessen/go-mail"
)

//go:embed templates
var templatesFS embed.FS

//go:embed seed
var seedFS embed.FS

//go:embed seed/init.sql
var initSql string

// TODO: Context management.
func main() {
	e := echo.New()
	e.Logger.SetLevel(log.DEBUG)
	e.Logger.SetOutput(os.Stdout)

	var seed bool
	flag.BoolVar(&seed, "seed", false, "migrate database and upload example files to s3")
	flag.Parse()

	e.Logger.Debug("initializing connections")
	h := NewHandler()
	defer h.Close()

	if seed {
		e.Logger.Debug("migrating db and seeding")
		_, err := h.db.ExecContext(context.TODO(), initSql)
		if err != nil {
			panic(err)
		}

		files, err := seedFS.ReadDir("seed")
		if err != nil {
			panic(err)
		}
		for _, f := range files {
			var file fs.File
			file, err = seedFS.Open(path.Join("seed", f.Name()))
			if err != nil {
				panic(err)
			}

			fileInfo, _ := f.Info()

			e.Logger.Debugf("inserting file: %s", f.Name())
			_, err = h.insertFile(context.TODO(), f.Name(), file, fileInfo.Size(), false)
			if err != nil {
				panic(err)
			}
		}
		return
	}

	e.Logger.Debug("configuring and running Echo")
	e.Renderer = new(Renderer)
	e.Static("/static", "static")
	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Output: os.Stdout,
	}))
	e.Use(middleware.CSRFWithConfig(middleware.CSRFConfig{
		TokenLookup:    "form:csrf",
		CookiePath:     "/",
		CookieHTTPOnly: true,
		CookieMaxAge:   15 * 60, // 15m
		// TODO: Set Domain attribute.
	}))
	e.Use(session.MiddlewareWithConfig(session.Config{
		Store: h.store,
	}))

	e.GET("/", func(c echo.Context) error {
		ctx := c.Request().Context()

		s, err := session.Get("session", c)
		if err != nil {
			return err
		}
		var seenFileIds []uint64
		if val, ok := s.Values["seen_files"].([]uint64); ok {
			seenFileIds = val
		}

		rows, err := h.db.QueryContext(ctx, "SELECT id, created_at, name, url, size FROM files ORDER BY created_at DESC LIMIT 5")
		if err != nil {
			return err
		}
		defer rows.Close()

		var files []File
		for rows.Next() {
			var f File
			err = rows.Scan(&f.ID, &f.CreatedAt, &f.Name, &f.Url, &f.Size)
			if err != nil {
				return err
			}
			if slices.Contains(seenFileIds, f.ID) {
				f.Seen = true
			}
			files = append(files, f)
		}

		err = rows.Err()
		if err != nil {
			return err
		}

		_ = rows.Close()

		return c.Render(http.StatusOK, "sites/list.html", map[string]any{
			"LatestFiles": files,
			"CsrfToken":   c.Get("csrf"),
		})
	})

	e.GET("/detail/:id", func(c echo.Context) error {
		ctx := c.Request().Context()

		c.Logger().Infof("viewing file: %s", c.Param("id"))

		var f File
		row := h.db.QueryRowContext(ctx, "SELECT id, created_at, name, url, size FROM files WHERE id=$1", c.Param("id"))
		err := row.Scan(&f.ID, &f.CreatedAt, &f.Name, &f.Url, &f.Size)
		if err != nil {
			return err
		}

		s, err := session.Get("session", c)
		if err != nil {
			return err
		}
		var seenFileIds []uint64
		if val, ok := s.Values["seen_files"].([]uint64); ok {
			seenFileIds = val
		}
		if !slices.Contains(seenFileIds, f.ID) {
			seenFileIds = append(seenFileIds, f.ID)
		}
		s.Values["seen_files"] = seenFileIds
		err = s.Save(c.Request(), c.Response())
		if err != nil {
			return err
		}

		return c.Render(http.StatusOK, "sites/detail.html", map[string]any{
			"File": f,
		})
	})

	e.POST("/upload", func(c echo.Context) error {
		ctx := c.Request().Context()
		fh, err := c.FormFile("file")
		if err != nil {
			return err
		}

		c.Logger().Infof("uploading file: %s", fh.Filename)

		ff, err := fh.Open()
		if err != nil {
			return err
		}
		defer ff.Close()

		id, err := h.insertFile(ctx, fh.Filename, ff, fh.Size, true)
		if err != nil {
			return err
		}

		return c.Redirect(http.StatusMovedPermanently, fmt.Sprintf("/detail/%d", id))
	})

	if err := e.Start(":8080"); err != nil {
		e.Logger.Fatal(err)
	}
}

type File struct {
	ID        uint64
	Name      string
	CreatedAt time.Time
	Size      int64
	Url       string
	Seen      bool
}

// TODO: Warm / cache templates for all possible paths.

type Renderer struct{}

func (r *Renderer) Render(w io.Writer, name string, data any, _ echo.Context) error {
	t, err := template.ParseFS(templatesFS, "templates/index.html", path.Join("templates", name), "templates/components/*.html")
	if err != nil {
		return err
	}
	return t.Execute(w, data)
}

type Handler struct {
	db     *sql.DB
	s3     *minio.Client
	mailer *mail.Client
	store  sessions.Store
}

func (h *Handler) Close() {
	_ = h.db.Close()
	_ = h.mailer.Close()
}

func NewHandler() *Handler {
	connString := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		os.Getenv("DB_HOST"),
		os.Getenv("DB_PORT"),
		os.Getenv("DB_USER"),
		os.Getenv("DB_PASSWORD"),
		"db",
	)
	db, err := sql.Open("postgres", connString)
	if err != nil {
		panic(err)
	}
	err = db.Ping()
	if err != nil {
		panic(err)
	}

	s3, err := minio.New(strings.TrimPrefix(os.Getenv("S3_ENDPOINT"), "https://"), &minio.Options{
		Creds: credentials.NewStaticV4(
			os.Getenv("S3_ACCESS_KEY_ID"),
			os.Getenv("S3_SECRET_ACCESS_KEY"),
			os.Getenv("S3_TOKEN"),
		),
		Secure: true,
	})
	if err != nil {
		panic(err)
	}

	mailerPort, err := strconv.Atoi(os.Getenv("SMTP_PORT"))
	if err != nil {
		panic(err)
	}
	mailer, err := mail.NewClient(
		os.Getenv("SMTP_HOST"),
		mail.WithTLSPortPolicy(mail.NoTLS),
		mail.WithPort(mailerPort),
	)
	if err != nil {
		panic(err)
	}

	redisClient := redis.NewClient(&redis.Options{
		Addr: fmt.Sprintf("%s:%s", os.Getenv("REDIS_HOST"), os.Getenv("REDIS_PORT")),
		DB:   1,
	})
	store, err := redisstore.NewRedisStore(context.TODO(), redisClient)
	if err != nil {
		panic(err)
	}
	store.Options(sessions.Options{
		Path:     "/",
		HttpOnly: true,
		MaxAge:   15 * 60, // 15m
		// TODO: Set Domain attribute.
	})

	return &Handler{
		db:     db,
		s3:     s3,
		mailer: mailer,
		store:  store,
	}
}

func (h *Handler) insertFile(ctx context.Context, filename string, reader io.Reader, size int64, sendEmail bool) (uint64, error) {
	objectKey := fmt.Sprintf("%d_%s", time.Now().Unix(), filename)

	info, err := h.s3.PutObject(ctx, os.Getenv("S3_BUCKET"), objectKey, reader, size, minio.PutObjectOptions{})
	if err != nil {
		return 0, err
	}

	var id uint64
	err = h.db.QueryRowContext(
		ctx, "INSERT INTO files (name, url, size) VALUES ($1, $2, $3) RETURNING id",
		filename,
		fmt.Sprintf("%s/%s/%s", os.Getenv("S3_ENDPOINT"), os.Getenv("S3_BUCKET"), info.Key),
		info.Size,
	).Scan(&id)
	if err != nil {
		return 0, err
	}

	if sendEmail {
		m := mail.NewMsg()
		_ = m.From("recipe@zerops.io")
		_ = m.To("recipient@example.com")
		m.Subject("File successfully uploaded")
		m.SetBodyString(mail.TypeTextPlain, fmt.Sprintf("File %s - %dB succesfully uploaded to s3.", info.Key, info.Size))
		err = h.mailer.DialAndSendWithContext(ctx, m)
		if err != nil {
			return 0, err
		}
	}

	return id, nil
}
