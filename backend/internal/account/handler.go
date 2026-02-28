package account

import (
	"errors"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type AccountHandler struct {
	accountService *AccountService
}

func NewAccountHandler(accountService *AccountService) *AccountHandler {
	return &AccountHandler{accountService: accountService}
}

// CreateAccount 处理用户注册请求
// 前端请求：POST /account/register
// 请求体：{"username": "alice", "password": "123456"}
func (h *AccountHandler) CreateAccount(c *gin.Context) {
	 // 1. 解析请求体到 CreateAccountRequest 结构体
	var req CreateAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		 // 解析失败，返回400错误
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	  // 2. 调用Service层创建账号
    // 传入用户名和密码，Service层会：
    // - 检查用户名是否已存在
    // - 对密码进行bcrypt哈希处理
    // - 将账号信息存入数据库
	if err := h.accountService.CreateAccount(c.Request.Context(), &Account{
		Username: req.Username,
		Password: req.Password,
	}); err != nil {
		// 注册失败（用户名已存在），返回500错误
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	// 注册成功，返回成功消息
	c.JSON(200, gin.H{"message": "account created"})
}

// Rename 处理用户改名请求
// 前端请求：POST /account/rename
// 请求体：{"new_username": "bob"}
// 请求头：Authorization: Bearer eyJhbGc...
func (h *AccountHandler) Rename(c *gin.Context) {
	// 1. 解析请求体到 RenameRequest 结构体
	var req RenameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// 解析失败，返回400错误
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	// 2. 从Gin上下文中获取当前用户ID
	accountID, err := getAccountID(c)
	if err != nil {
		  // 未登录，返回400错误
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	 
    // 3. 调用Service层处理改名逻辑
    // Service层会：
    // - 生成新的JWT Token（因为用户名变了）
    // - 更新数据库中的用户名和Token（在同一事务中）
    // - 更新Redis缓存中的Token
	token, err := h.accountService.Rename(c.Request.Context(), accountID, req.NewUsername)
	if err != nil {
		 // 根据不同的错误类型返回不同的HTTP状态码
		if errors.Is(err, ErrNewUsernameRequired) {
			 // 新用户名为空，返回400错误
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, ErrUsernameTaken) {
			// 用户名已被占用，返回409错误
			c.JSON(409, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 用户不存在，返回404错误
			c.JSON(404, gin.H{"error": "account not found"})
			return
		}
		// 其他错误，返回500错误
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	 // 改名成功，返回新的Token
    // 注意：旧Token立即失效，前端需要替换Token
	c.JSON(200, gin.H{"token": token})
}

// ChangePassword 处理修改密码请求
// 前端请求：POST /account/changePassword
// 请求体：{"username": "alice", "old_password": "123456", "new_password": "654321"}
func (h *AccountHandler) ChangePassword(c *gin.Context) {
	// 1. 解析请求体到 ChangePasswordRequest 结构体
	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		 // 解析失败，返回400错误
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	 // 2. 调用Service层处理修改密码逻辑
    // Service层会：
    // - 验证旧密码是否正确
    // - 对新密码进行bcrypt哈希处理
    // - 更新数据库中的密码
    // - 清空Token（强制所有设备下线）
    // - 删除Redis缓存中的Token
	if err := h.accountService.ChangePassword(c.Request.Context(), req.Username, req.OldPassword, req.NewPassword); err != nil {
		c.JSON(400, gin.H{"error": "unsuccessfully password changed"})
		return
	}
	c.JSON(200, gin.H{"message": "successfully password changed"})
}

// FindByID 处理按ID查询用户请求
// 前端请求：POST /account/findByID
// 请求体：{"id": 1}
func (h *AccountHandler) FindByID(c *gin.Context) {
	 // 1. 解析请求体到 FindByIDRequest 结构体
	var req FindByIDRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	 // 2. 调用Service层查询用户
	if account, err := h.accountService.FindByID(c.Request.Context(), req.ID); err != nil {
		// 查询失败，返回500错误
		c.JSON(500, gin.H{"error": err.Error()})
		return
	} else {
		// 查询成功，返回用户信息（不包含密码和Token）
		c.JSON(200, account)
	}
}

// FindByUsername 处理按用户名查询用户请求
// 前端请求：POST /account/findByUsername
// 请求体：{"username": "alice"}
func (h *AccountHandler) FindByUsername(c *gin.Context) {
	// 1. 解析请求体到 FindByUsernameRequest 结构体
	var req FindByUsernameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	// 2. 调用Service层查询用户
	if account, err := h.accountService.FindByUsername(c.Request.Context(), req.Username); err != nil {
		  // 查询失败，返回500错误
		c.JSON(500, gin.H{"error": err.Error()})
		return
	} else {
		// 查询成功，返回用户信息
		c.JSON(200, account)
	}
}

// Login 处理用户登录请求
// 前端请求：POST /account/login
// 请求体：{"username": "alice", "password": "123456"}
func (h *AccountHandler) Login(c *gin.Context) {
	 // 1. 解析请求体到 LoginRequest 结构体
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// 解析失败，返回400错误
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	
  // 2. 调用Service层处理登录逻辑
  // 传入用户名和密码，Service层会：
  // - 查询数据库验证用户是否存在
  // - 比对密码哈希是否正确
  // - 生成JWT Token
  // - 将Token存入数据库和Redis缓存
	if token, err := h.accountService.Login(c.Request.Context(), req.Username, req.Password); err != nil {
		 // 登录失败（用户不存在或密码错误），返回500错误
		c.JSON(500, gin.H{"error": err.Error()})
		return
	} else {
		 // 登录成功，返回Token给前端
		c.JSON(200, gin.H{"token": token})
	}
}

// Logout 处理用户登出请求
// 前端请求：POST /account/logout
// 请求头：Authorization: Bearer eyJhbGc...
func (h *AccountHandler) Logout(c *gin.Context) {
	// 1. 从Gin上下文中获取当前用户ID
  // 这个ID是由JWTAuth中间件验证Token后设置的
	accountID, err := getAccountID(c)
	if err != nil {
		// 未登录（上下文中没有accountID），返回400错误
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	// 2. 调用Service层处理登出逻辑
  // Service层会：
  // - 清空数据库中的Token字段
  // - 删除Redis缓存中的Token
	if err := h.accountService.Logout(c.Request.Context(), accountID); err != nil {
		 // 登出失败，返回500错误
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	 // 登出成功，返回成功消息
	c.JSON(200, gin.H{"message": "account logged out"})
}

// getAccountID 从Gin上下文中获取当前用户ID
// 这个ID是由JWTAuth中间件验证Token后设置的
func getAccountID(c *gin.Context) (uint, error) {
	// 1. 从上下文中获取 "accountID" 值
	value, exists := c.Get("accountID")
	if !exists {
		// 上下文中没有accountID，说明未登录
		return 0, errors.New("accountID not found")
	}
	// 2. 类型断言，将 interface{} 转换为 uint
	id, ok := value.(uint)
	if !ok {
		// 类型转换失败
		return 0, errors.New("accountID has invalid type")
	}
	 // 3. 返回用户ID
	return id, nil
}
