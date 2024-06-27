package main

import (
	"embed"
	"flag"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/sessions"
	_ "github.com/joho/godotenv/autoload"
	"github.com/labstack/echo-contrib/session"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/labstack/gommon/log"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/wneessen/go-mail"
)

// TODO(tikinang):
//  - logging
//  - sessions -> storing number of viewed images, how are you feeling today
//  - storing to s3
//  - htmx

//go:embed templates
var templatesFs embed.FS

func main() {
	var seed bool
	flag.BoolVar(&seed, "seed", false, "migrate database and upload example files to s3")
	flag.Parse()

	var h *Handler
	{
		var err error
		h, err = handler()
		if err != nil {
			panic(err)
		}
	}

	if seed {
		return
	}

	e := echo.New()
	e.Logger.SetLevel(log.DEBUG)
	e.Renderer = new(Renderer)
	e.Static("/static", "static")
	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Output: os.Stdout,
	}))
	e.Use(middleware.CSRFWithConfig(middleware.CSRFConfig{
		TokenLookup: "form:csrf",
	}))
	e.Use(session.MiddlewareWithConfig(session.Config{
		Store: sessions.NewCookieStore([]byte(os.Getenv("SESSIONS_SECRET"))),
	}))

	e.GET("/", func(c echo.Context) error {
		ctx := c.Request().Context()

		var files []File
		objects := h.s3.ListObjects(ctx, os.Getenv("S3_BUCKET"), minio.ListObjectsOptions{Recursive: true})
		for object := range objects {
			files = append(files, File{
				Name:      object.Key,
				CreatedAt: object.LastModified,
				Size:      object.Size,
				Url:       fmt.Sprintf("%s/%s/%s", os.Getenv("S3_ENDPOINT"), os.Getenv("S3_BUCKET"), object.Key),
			})
		}

		return c.Render(http.StatusOK, "sites/list.html", map[string]any{
			"LatestFiles": files,
			"CsrfToken":   c.Get("csrf"),
		})
	})

	e.GET("/detail/:filename", func(c echo.Context) error {
		ctx := c.Request().Context()

		object, err := h.s3.GetObjectACL(ctx, os.Getenv("S3_BUCKET"), c.Param("filename"))
		if err != nil {
			return err
		}

		return c.Render(http.StatusOK, "sites/detail.html", map[string]any{
			"File": File{
				Name:      object.Key,
				CreatedAt: object.LastModified,
				Size:      object.Size,
				Url:       fmt.Sprintf("%s/%s/%s", os.Getenv("S3_ENDPOINT"), os.Getenv("S3_BUCKET"), object.Key),
			},
		})
	})

	e.POST("/upload", func(c echo.Context) error {
		ctx := c.Request().Context()
		fh, err := c.FormFile("file")
		if err != nil {
			return err
		}

		ff, err := fh.Open()
		if err != nil {
			return err
		}
		defer ff.Close()

		info, err := h.s3.PutObject(ctx, os.Getenv("S3_BUCKET"), fh.Filename, ff, -1, minio.PutObjectOptions{})
		if err != nil {
			return err
		}
		_ = info

		m := mail.NewMsg()
		_ = m.From("recipe@zerops.io")
		_ = m.To("recipient@example.com")
		m.Subject("File successfully uploaded")
		m.SetBodyString(mail.TypeTextPlain, fmt.Sprintf("File %s - %dB succesfully uploaded to s3.", info.Key, info.Size))
		err = h.mailer.DialAndSendWithContext(ctx, m)
		if err != nil {
			return err
		}

		return c.Redirect(http.StatusMovedPermanently, fmt.Sprintf("/detail/%s", url.PathEscape(info.Key)))
	})

	if err := e.Start(":8080"); err != nil {
		e.Logger.Fatal(err)
	}
}

type File struct {
	Name      string
	CreatedAt time.Time
	Size      int64
	Url       string
}

// TODO(tikinang): Warm / cache templates for all possible paths.

type Renderer struct{}

func (r *Renderer) Render(w io.Writer, name string, data any, _ echo.Context) error {
	t, err := template.ParseFS(templatesFs, "templates/index.html", path.Join("templates", name), "templates/components/*.html")
	if err != nil {
		return err
	}
	return t.Execute(w, data)
}

type Handler struct {
	s3     *minio.Client
	mailer *mail.Client
}

func handler() (*Handler, error) {
	s3, err := minio.New(strings.TrimPrefix(os.Getenv("S3_ENDPOINT"), "https://"), &minio.Options{
		Creds: credentials.NewStaticV4(
			os.Getenv("S3_ACCESS_KEY_ID"),
			os.Getenv("S3_SECRET_ACCESS_KEY"),
			os.Getenv("S3_TOKEN"),
		),
		Secure: true,
	})
	if err != nil {
		return nil, err
	}

	mailerPort, err := strconv.Atoi(os.Getenv("SMTP_PORT"))
	if err != nil {
		return nil, err
	}
	mailer, err := mail.NewClient(
		os.Getenv("SMTP_HOST"),
		mail.WithTLSPortPolicy(mail.NoTLS),
		mail.WithPort(mailerPort),
	)
	if err != nil {
		return nil, err
	}

	return &Handler{
		s3:     s3,
		mailer: mailer,
	}, nil
}
