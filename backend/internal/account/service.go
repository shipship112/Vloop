package account

import (
	"context"
	"errors"
	"feedsystem_video_go/internal/auth"
	"fmt"
	"log"
	"time"

	rediscache "feedsystem_video_go/internal/middleware/redis"

	"github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// AccountService 账户服务层，处理业务逻辑
// - 职责：业务规则、缓存管理、事务协调
// - 不直接操作HTTP或数据库，通过Repository和Cache完成
type AccountService struct {
	accountRepository *AccountRepository // 账户仓储层，负责数据库操作
	cache             *rediscache.Client // Redis缓存客户端，用于缓存账户token信息
}

var (
	ErrUsernameTaken       = errors.New("username already exists") // 用户名已被占用
	ErrNewUsernameRequired = errors.New("new_username is required") // 新用户名不能为空
)

// NewAccountService 创建账户服务实例
// 参数：
//   - accountRepository: 账户仓储层，用于数据库操作
//   - cache: Redis缓存客户端，用于缓存token等数据
func NewAccountService(accountRepository *AccountRepository, cache *rediscache.Client) *AccountService {
	return &AccountService{accountRepository: accountRepository, cache: cache}
}

// CreateAccount 创建新账户
// 业务流程：
// 1. 使用bcrypt对密码进行哈希加密（ bcrypt.DefaultCost = 10 ）
// 2. 调用Repository层将账户信息存入数据库
// 参数：
//   - ctx: 上下文，用于控制请求超时和取消
//   - account: 待创建的账户信息（包含明文密码）
func (as *AccountService) CreateAccount(ctx context.Context, account *Account) error {
	// 使用bcrypt对密码进行哈希加密，防止明文存储
	// bcrypt.DefaultCost = 10，即2^10=1024次轮询加密
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(account.Password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	// 将哈希后的密码赋值回account对象
	account.Password = string(passwordHash)

	// 调用Repository层将账户信息存入数据库
	if err := as.accountRepository.CreateAccount(ctx, account); err != nil {
		return err
	}
	return nil
}

// Rename 修改用户名并生成新token
// 业务流程：
// 1. 校验新用户名不能为空
// 2. 基于新用户名生成新的JWT token
// 3. 在数据库事务中更新用户名和token
// 4. 将新token存入Redis缓存（24小时过期）
// 参数：
//   - ctx: 上下文
//   - accountID: 账户ID
//   - newUsername: 新用户名
// 返回：
//   - string: 新生成的JWT token
//   - error: 错误信息
func (as *AccountService) Rename(ctx context.Context, accountID uint, newUsername string) (string, error) {
	// 校验新用户名不能为空
	if newUsername == "" {
		return "", ErrNewUsernameRequired
	}

	// 基于账户ID和新用户名生成新的JWT token
	token, err := auth.GenerateToken(accountID, newUsername)
	if err != nil {
		return "", err
	}

	// 调用Repository层在数据库事务中更新用户名和token
	if err := as.accountRepository.RenameWithToken(ctx, accountID, newUsername, token); err != nil {
		// 处理MySQL唯一索引冲突（用户名已存在）
		var mysqlErr *mysql.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return "", ErrUsernameTaken
		}
		// 处理账户不存在的情况
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", err
		}
		return "", err
	}

	// 将新token存入Redis缓存（缓存键格式：account:{accountID}）
	if as.cache != nil {
		cacheCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()

		if err := as.cache.SetBytes(cacheCtx, fmt.Sprintf("account:%d", accountID), []byte(token), 24*time.Hour); err != nil {
			log.Printf("failed to set cache: %v", err)
		}
	}
	return token, nil
}

// ChangePassword 修改密码
// 业务流程：
// 1. 根据用户名查询账户信息
// 2. 验证旧密码是否正确（使用bcrypt对比）
// 3. 使用bcrypt对新密码进行哈希加密
// 4. 更新数据库中的密码
// 5. 执行登出操作（清除旧token）
// 参数：
//   - ctx: 上下文
//   - username: 用户名
//   - oldPassword: 旧密码（明文）
//   - newPassword: 新密码（明文）
func (as *AccountService) ChangePassword(ctx context.Context, username, oldPassword, newPassword string) error {
	// 根据用户名查询账户信息
	account, err := as.FindByUsername(ctx, username)
	if err != nil {
		return err
	}

	// 验证旧密码是否正确（bcrypt对比）
	// CompareHashAndPassword会自动处理bcrypt的salt，无需手动处理
	if err := bcrypt.CompareHashAndPassword([]byte(account.Password), []byte(oldPassword)); err != nil {
		return err
	}

	// 使用bcrypt对新密码进行哈希加密
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	// 更新数据库中的密码
	if err := as.accountRepository.ChangePassword(ctx, account.ID, string(passwordHash)); err != nil {
		return err
	}

	// 执行登出操作（清除旧token）
	if err := as.Logout(ctx, account.ID); err != nil {
		return err
	}
	return nil
}

// FindByID 根据账户ID查询账户信息
// 参数：
//   - ctx: 上下文
//   - id: 账户ID
// 返回：
//   - *Account: 账户信息指针
//   - error: 错误信息
func (as *AccountService) FindByID(ctx context.Context, id uint) (*Account, error) {
	if account, err := as.accountRepository.FindByID(ctx, id); err != nil {
		return nil, err
	} else {
		return account, nil
	}
}

// FindByUsername 根据用户名查询账户信息
// 参数：
//   - ctx: 上下文
//   - username: 用户名
// 返回：
//   - *Account: 账户信息指针
//   - error: 错误信息
func (as *AccountService) FindByUsername(ctx context.Context, username string) (*Account, error) {
	if account, err := as.accountRepository.FindByUsername(ctx, username); err != nil {
		return nil, err
	} else {
		return account, nil
	}
}

// Login 用户登录
// 业务流程：
// 1. 根据用户名查询账户信息
// 2. 使用bcrypt验证密码是否正确
// 3. 生成JWT token（包含账户ID和用户名）
// 4. 将token存入数据库（用于后续的软鉴权和登出操作）
// 5. 将token存入Redis缓存（缓存键格式：account:{accountID}，有效期24小时）
// 参数：
//   - ctx: 上下文
//   - username: 用户名
//   - password: 密码（明文）
// 返回：
//   - string: JWT token
//   - error: 错误信息
func (as *AccountService) Login(ctx context.Context, username, password string) (string, error) {
	// 根据用户名查询账户信息
	account, err := as.FindByUsername(ctx, username)
	if err != nil {
		return "", err
	}

	// 使用bcrypt验证密码是否正确
	if err := bcrypt.CompareHashAndPassword([]byte(account.Password), []byte(password)); err != nil {
		return "", err
	}

	// 生成JWT token（包含账户ID和用户名）
	token, err := auth.GenerateToken(account.ID, account.Username)
	if err != nil {
		return "", err
	}

	// 将token存入数据库（用于后续的软鉴权和登出操作）
	if err := as.accountRepository.Login(ctx, account.ID, token); err != nil {
		return "", err
	}

	// 将token存入Redis缓存（缓存键格式：account:{accountID}，有效期24小时）
	if as.cache != nil {
		cacheCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()

		if err := as.cache.SetBytes(cacheCtx, fmt.Sprintf("account:%d", account.ID), []byte(token), 24*time.Hour); err != nil {
			log.Printf("failed to set cache: %v", err)
		}
	}
	return token, nil
}

// Logout 用户登出
// 业务流程：
// 1. 查询账户信息，检查是否已登录（token是否为空）
// 2. 删除Redis缓存中的token
// 3. 将数据库中的token字段置空（使之前的JWT token失效）
// 参数：
//   - ctx: 上下文
//   - accountID: 账户ID
func (as *AccountService) Logout(ctx context.Context, accountID uint) error {
	// 查询账户信息
	account, err := as.FindByID(ctx, accountID)
	if err != nil {
		return err
	}

	// 如果token为空，说明已经登出，无需处理
	if account.Token == "" {
		return nil
	}

	// 删除Redis缓存中的token
	if as.cache != nil {
		cacheCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()

		if err := as.cache.Del(cacheCtx, fmt.Sprintf("account:%d", account.ID)); err != nil {
			log.Printf("failed to del cache: %v", err)
		}
	}

	// 将数据库中的token字段置空（使之前的JWT token失效）
	return as.accountRepository.Logout(ctx, account.ID)
}
