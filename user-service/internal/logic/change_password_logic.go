package logic

import (
	"context"
	"errors"

	"seckill-mall/user-service/internal/model"
	"seckill-mall/user-service/internal/svc"
	"seckill-mall/user-service/user"

	"github.com/zeromicro/go-zero/core/logx"
	"golang.org/x/crypto/bcrypt"
)

type ChangePasswordLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewChangePasswordLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ChangePasswordLogic {
	return &ChangePasswordLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// ChangePassword 修改密码
func (l *ChangePasswordLogic) ChangePassword(in *user.ChangePasswordRequest) (*user.BoolResponse, error) {
	// 参数校验
	if in.UserId <= 0 {
		return nil, errors.New("用户ID无效")
	}
	if in.OldPassword == "" || in.NewPassword == "" {
		return nil, errors.New("旧密码和新密码不能为空")
	}
	if len(in.NewPassword) < 6 {
		return nil, errors.New("新密码长度不能少于6位")
	}

	// 查询用户
	existingUser, err := l.svcCtx.UserModel.FindOneById(l.ctx, in.UserId)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return nil, errors.New("用户不存在")
		}
		l.Logger.Errorf("查询用户失败: %v", err)
		return nil, errors.New("系统错误，请稍后重试")
	}

	// 验证旧密码
	if err := bcrypt.CompareHashAndPassword([]byte(existingUser.Password), []byte(in.OldPassword)); err != nil {
		l.Logger.Infof("旧密码错误: userId=%d", in.UserId)
		return nil, errors.New("旧密码错误")
	}

	// 加密新密码
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(in.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		l.Logger.Errorf("密码加密失败: %v", err)
		return nil, errors.New("系统错误，请稍后重试")
	}

	// 更新密码
	if err := l.svcCtx.UserModel.UpdatePassword(l.ctx, in.UserId, string(hashedPassword)); err != nil {
		l.Logger.Errorf("更新密码失败: %v", err)
		return nil, errors.New("更新密码失败，请稍后重试")
	}

	l.Logger.Infof("密码修改成功: userId=%d", in.UserId)

	return &user.BoolResponse{
		Success: true,
		Message: "密码修改成功",
	}, nil
}
