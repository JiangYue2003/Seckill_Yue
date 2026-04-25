package logic

import (
	"context"
	"errors"
	"time"

	"seckill-mall/common/user"
	"seckill-mall/user-service/internal/model"
	"seckill-mall/user-service/internal/model/entity"
	"seckill-mall/user-service/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
	"golang.org/x/crypto/bcrypt"
)

type RegisterLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewRegisterLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RegisterLogic {
	return &RegisterLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// Register 用户注册
func (l *RegisterLogic) Register(in *user.RegisterRequest) (*user.UserInfo, error) {
	// 参数校验
	if len(in.Username) < 3 || len(in.Username) > 50 {
		return nil, errors.New("用户名长度必须在3-50个字符之间")
	}
	if len(in.Password) < 6 || len(in.Password) > 128 {
		return nil, errors.New("密码长度必须在6-128个字符之间")
	}
	if in.Email == "" {
		return nil, errors.New("邮箱不能为空")
	}

	// 检查用户名是否已存在
	existingUser, err := l.svcCtx.UserModel.FindOneByUsername(l.ctx, in.Username)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		l.Logger.Errorf("查询用户名失败: %v", err)
		return nil, errors.New("系统错误，请稍后重试")
	}
	if existingUser != nil {
		return nil, errors.New("用户名已存在")
	}

	// 检查邮箱是否已存在
	existingEmail, err := l.svcCtx.UserModel.FindOneByEmail(l.ctx, in.Email)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		l.Logger.Errorf("查询邮箱失败: %v", err)
		return nil, errors.New("系统错误，请稍后重试")
	}
	if existingEmail != nil {
		return nil, errors.New("邮箱已被注册")
	}

	// 密码加密
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		l.Logger.Errorf("密码加密失败: %v", err)
		return nil, errors.New("系统错误，请稍后重试")
	}

	// 创建用户
	newUser := &entity.User{
		Username:  in.Username,
		Password:  string(hashedPassword),
		Email:     in.Email,
		Phone:     in.Phone,
		Status:    1,
		CreatedAt: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
	}

	if err := l.svcCtx.UserModel.Insert(l.ctx, newUser); err != nil {
		l.Logger.Errorf("创建用户失败: %v", err)
		return nil, errors.New("创建用户失败，请稍后重试")
	}

	l.Logger.Infof("用户注册成功: userId=%d, username=%s", newUser.ID, newUser.Username)

	return &user.UserInfo{
		Id:        newUser.ID,
		Username:  newUser.Username,
		Email:     newUser.Email,
		Phone:     newUser.Phone,
		Status:    newUser.Status,
		CreatedAt: newUser.CreatedAt,
		UpdatedAt: newUser.UpdatedAt,
	}, nil
}
