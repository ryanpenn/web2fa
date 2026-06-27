package main

import (
	"embed"
	"net/http"
	"strings"
	"text/template"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pquerna/otp/totp"
)

//go:embed web/index.html
var htmlFS embed.FS

const (
	period = uint(30) // TOTP 默认周期
	skew   = uint(1)  // 允许的时间窗口偏移
)

func main() {
	r := setupRouter()
	if err := r.Run(":8081"); err != nil {
		panic(err)
	}
}

func setupRouter() *gin.Engine {
	r := gin.Default()
	tmpl := template.Must(
		template.ParseFS(htmlFS, "web/index.html"),
	)

	r.GET("/", func(c *gin.Context) {
		renderPage(c, tmpl, "home", "")
	})

	// 生成 TOTP，返回 JSON 包含 code 和当前秒数
	r.GET("/generate", func(c *gin.Context) {
		secret := parseSecret(c.Query("secret"))
		if secret == "" {
			c.JSON(http.StatusBadRequest, gin.H{"code": "------", "secondsElapsed": 0})
			return
		}
		now := time.Now()
		code, err := totp.GenerateCodeCustom(secret, now, totp.ValidateOpts{Period: period, Skew: skew})
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"code": "错误", "secondsElapsed": 0})
			return
		}
		secondsElapsed := int(now.Unix()) % int(period)
		c.JSON(http.StatusOK, gin.H{"code": code, "secondsElapsed": secondsElapsed})
	})

	r.GET("/list", func(c *gin.Context) {
		renderPage(c, tmpl, "list", "")
	})

	r.GET("/:secret", func(c *gin.Context) {
		secret := strings.TrimSpace(c.Param("secret"))
		if secret == "" || secret == "verify" {
			c.Status(http.StatusNotFound)
			return
		}
		renderPage(c, tmpl, "single", secret)
	})

	return r
}

func renderPage(c *gin.Context, tmpl *template.Template, view string, secret string) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	_ = tmpl.Execute(c.Writer, gin.H{
		"Period": period,
		"View":   view,
		"Secret": secret,
	})
}

func parseSecret(secret string) string {
	s := strings.TrimSpace(secret)
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "-", "")
	return s
}
