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
	r := gin.Default()

	tmpl := template.Must(
		template.ParseFS(htmlFS, "web/index.html"),
	)

	r.GET("/", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		_ = tmpl.Execute(c.Writer, gin.H{
			"Period": period,
		})
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

	// 验证 TOTP
	r.GET("/verify", func(c *gin.Context) {
		secret := parseSecret(c.Query("secret"))
		code := strings.TrimSpace(c.Query("code"))
		valid := totp.Validate(code, secret)
		c.String(http.StatusOK, map[bool]string{
			true:  "true",
			false: "false",
		}[valid])
	})

	r.Run(":8080")
}

func parseSecret(secret string) string {
	s := strings.ReplaceAll(secret, " ", "")
	s = strings.ReplaceAll(s, "-", "")
	return s
}
