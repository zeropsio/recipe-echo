package main

import (
	"fmt"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"net/http"
)

// TODO(tikinang):
//  - logging
//  - sessions
//  -

func main() {
	r := gin.Default()

	store := cookie.NewStore([]byte("secret"))
	r.Use(sessions.Sessions("xxx", store))

	r.GET("/ping", func(c *gin.Context) {
		session := sessions.Default(c)

		if session.Get("hello") != "world" {
			fmt.Println("setting session")
			session.Set("hello", "world")
			session.Save()
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "pong",
		})
	})
	r.Run()
}
