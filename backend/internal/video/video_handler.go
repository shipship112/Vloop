package video

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"feedsystem_video_go/internal/account"
	"feedsystem_video_go/internal/middleware/jwt"

	"github.com/gin-gonic/gin"
)

// VideoHandler 视频处理器，负责处理视频相关的HTTP请求
type VideoHandler struct {
	service        *VideoService        // 视频服务层，处理视频业务逻辑
	accountService *account.AccountService // 账户服务层，查询账户信息
}

// NewVideoHandler 创建视频处理器实例
func NewVideoHandler(service *VideoService, accountService *account.AccountService) *VideoHandler {
	return &VideoHandler{service: service, accountService: accountService}
}

// PublishVideo 发布视频接口
// 路由：POST /video/publish
// 功能：使用已上传的视频和封面URL创建视频记录
// 请求体：{"title": "标题", "description": "描述", "play_url": "视频URL", "cover_url": "封面URL"}
func (vh *VideoHandler) PublishVideo(c *gin.Context) {
	// 1. 解析JSON请求体
	var req PublishVideoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 2. 从JWT中间件中获取当前登录用户的ID
	authorId, err := jwt.GetAccountID(c)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 3. 查询用户信息（获取用户名）
	user, err := vh.accountService.FindByID(c.Request.Context(), authorId)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 4. 构造Video对象
	video := &Video{
		AuthorID:    authorId,              // 作者ID
		Username:    user.Username,         // 作者用户名（冗余存储，便于查询）
		Title:       req.Title,             // 视频标题
		Description: req.Description,       // 视频描述
		PlayURL:     req.PlayURL,           // 播放地址
		CoverURL:    req.CoverURL,          // 封面地址
		CreateTime:  time.Now(),           // 创建时间
	}

	// 5. 调用Service层发布视频
	if err := vh.service.Publish(c.Request.Context(), video); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 6. 返回创建的视频信息
	c.JSON(200, video)
}

// UploadVideo 上传视频文件接口
// 路由：POST /video/upload
// 功能：接收MP4视频文件，保存到本地并返回访问URL
// 请求格式：multipart/form-data，字段名：file
func (vh *VideoHandler) UploadVideo(c *gin.Context) {
	// 1. 从JWT中间件获取当前登录用户ID
	authorId, err := jwt.GetAccountID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 2. 获取上传的文件
	f, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing file"})
		return
	}

	// 3. 验证文件大小（限制200MB）
	const maxSize = 200 << 20 // 200 * 1024 * 1024
	if f.Size <= 0 || f.Size > maxSize {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid file size"})
		return
	}

	// 4. 验证文件格式（仅允许.mp4）
	ext := strings.ToLower(filepath.Ext(f.Filename))
	if ext != ".mp4" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "only .mp4 is allowed"})
		return
	}

	// 5. 构造保存路径：.run/uploads/videos/{用户ID}/{日期}/
	date := time.Now().Format("20060102")
	relDir := filepath.Join("videos", fmt.Sprintf("%d", authorId), date)
	root := filepath.Join(".run", "uploads")
	absDir := filepath.Join(root, relDir)
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 6. 生成随机文件名（16位十六进制字符串 + 扩展名）
	filename := randHex(16) + ext
	absPath := filepath.Join(absDir, filename)

	// 7. 保存文件到磁盘
	if err := c.SaveUploadedFile(f, absPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 8. 构造访问URL：/static/videos/{用户ID}/{日期}/{文件名}
	urlPath := path.Join("/static", "videos", fmt.Sprintf("%d", authorId), date, filename)

	// 9. 返回完整URL
	c.JSON(http.StatusOK, gin.H{
		"url":      buildAbsoluteURL(c, urlPath), // 完整URL（含协议和域名）
		"play_url": buildAbsoluteURL(c, urlPath), // 播放URL（同url）
	})
}

// UploadCover 上传封面图片接口
// 路由：POST /video/cover
// 功能：接收图片文件（jpg/jpeg/png/webp），保存到本地并返回访问URL
// 请求格式：multipart/form-data，字段名：file
func (vh *VideoHandler) UploadCover(c *gin.Context) {
	// 1. 从JWT中间件获取当前登录用户ID
	authorId, err := jwt.GetAccountID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 2. 获取上传的文件
	f, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing file"})
		return
	}

	// 3. 验证文件大小（限制10MB）
	const maxSize = 10 << 20 // 10 * 1024 * 1024
	if f.Size <= 0 || f.Size > maxSize {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid file size"})
		return
	}

	// 4. 验证文件格式（仅允许.jpg/.jpeg/.png/.webp）
	ext := strings.ToLower(filepath.Ext(f.Filename))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp":
		// 允许的格式
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "only .jpg/.jpeg/.png/.webp is allowed"})
		return
	}

	// 5. 构造保存路径：.run/uploads/covers/{用户ID}/{日期}/
	date := time.Now().Format("20060102")
	relDir := filepath.Join("covers", fmt.Sprintf("%d", authorId), date)
	root := filepath.Join(".run", "uploads")
	absDir := filepath.Join(root, relDir)
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 6. 生成随机文件名
	filename := randHex(16) + ext
	absPath := filepath.Join(absDir, filename)

	// 7. 保存文件到磁盘
	if err := c.SaveUploadedFile(f, absPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 8. 构造访问URL：/static/covers/{用户ID}/{日期}/{文件名}
	urlPath := path.Join("/static", "covers", fmt.Sprintf("%d", authorId), date, filename)

	// 9. 返回完整URL
	c.JSON(http.StatusOK, gin.H{
		"url":       buildAbsoluteURL(c, urlPath),  // 完整URL
		"cover_url": buildAbsoluteURL(c, urlPath), // 封面URL（同url）
	})
}

// randHex 生成n字节的随机十六进制字符串
// 参数：n - 字节长度，例如n=16将生成32位十六进制字符串
// 返回：随机十六进制字符串
func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b) // 读取随机字节
	return hex.EncodeToString(b) // 转换为十六进制字符串
}

// buildAbsoluteURL 根据相对路径构建完整的URL
// 支持检测HTTPS协议和反向代理的X-Forwarded-Proto头
// 参数：
//   - c: gin上下文
//   - p: 相对路径（如 "/static/videos/..."）
// 返回：完整URL（如 "http://localhost:8080/static/videos/..."）
func buildAbsoluteURL(c *gin.Context, p string) string {
	// 默认使用http协议
	scheme := "http"

	// 如果使用了TLS（HTTPS），则使用https协议
	if c.Request.TLS != nil {
		scheme = "https"
	}

	// 如果有反向代理（如Nginx），从X-Forwarded-Proto头获取协议
	if xf := c.GetHeader("X-Forwarded-Proto"); xf != "" {
		scheme = xf
	}

	// 拼接完整URL：{协议}://{域名}{路径}
	return fmt.Sprintf("%s://%s%s", scheme, c.Request.Host, p)
}

// DeleteVideo 删除视频接口
// 路由：POST /video/delete
// 功能：删除指定ID的视频（仅允许删除自己发布的视频）
// 请求体：{"id": 视频ID}
func (vh *VideoHandler) DeleteVideo(c *gin.Context) {
	// 1. 解析JSON请求体
	var req DeleteVideoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 2. 从JWT中间件获取当前登录用户ID
	authorId, err := jwt.GetAccountID(c)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 3. 调用Service层删除视频（会验证是否为作者本人）
	if err := vh.service.Delete(c.Request.Context(), req.ID, authorId); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 4. 返回成功消息
	c.JSON(200, gin.H{"message": "video deleted"})
}

// ListByAuthorID 查询作者的视频列表接口
// 路由：POST /video/list-by-author
// 功能：根据作者ID查询该作者发布的所有视频
// 请求体：{"author_id": 作者ID}
func (vh *VideoHandler) ListByAuthorID(c *gin.Context) {
	// 1. 解析JSON请求体
	var req ListByAuthorIDRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 2. 调用Service层查询视频列表
	videos, err := vh.service.ListByAuthorID(c.Request.Context(), req.AuthorID)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 3. 返回视频列表
	c.JSON(200, videos)
}

// GetDetail 获取视频详情接口
// 路由：POST /video/detail
// 功能：根据视频ID获取视频完整信息（含缓存逻辑）
// 请求体：{"id": 视频ID}
func (vh *VideoHandler) GetDetail(c *gin.Context) {
	// 1. 解析JSON请求体
	var req GetDetailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 2. 调用Service层获取视频详情（含缓存逻辑）
	video, err := vh.service.GetDetail(c.Request.Context(), req.ID)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 3. 返回视频详情
	c.JSON(200, video)
}

// UpdateLikesCount 更新视频点赞数接口
// 路由：POST /video/update-likes
// 功能：更新视频的点赞数（供Worker异步调用，一般不直接暴露给前端）
// 请求体：{"id": 视频ID, "likes_count": 点赞数}
func (vh *VideoHandler) UpdateLikesCount(c *gin.Context) {
	// 1. 解析JSON请求体
	var req UpdateLikesCountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 2. 调用Service层更新点赞数
	if err := vh.service.UpdateLikesCount(c.Request.Context(), req.ID, req.LikesCount); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 3. 返回成功消息
	c.JSON(200, gin.H{"message": "likes count updated"})
}
