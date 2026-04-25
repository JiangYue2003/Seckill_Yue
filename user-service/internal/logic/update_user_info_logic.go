package logic

import (
	"context"
	"errors"
	"time"

	commonpb "seckill-mall/common/common"
	"seckill-mall/common/user"
	"seckill-mall/user-service/internal/model"
	"seckill-mall/user-service/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type UpdateUserInfoLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUpdateUserInfoLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateUserInfoLogic {
	return &UpdateUserInfoLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// UpdateUserInfo 更新用户信息
func (l *UpdateUserInfoLogic) UpdateUserInfo(in *user.UpdateUserInfoRequest) (*commonpb.BoolResponse, error) {
	// 参数校验
	if in.UserId <= 0 {
		return nil, errors.New("用户ID无效")
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

	// 更新字段
	if in.Email != "" {
		// 检查邮箱是否已被其他用户使用
		existingEmail, err := l.svcCtx.UserModel.FindOneByEmail(l.ctx, in.Email)
		if err != nil && !errors.Is(err, model.ErrNotFound) {
			l.Logger.Errorf("查询邮箱失败: %v", err)
			return nil, errors.New("系统错误，请稍后重试")
		}
		if existingEmail != nil && existingEmail.ID != in.UserId {
			return nil, errors.New("邮箱已被其他用户使用")
		}
		existingUser.Email = in.Email
	}

	if in.Phone != "" {
		existingUser.Phone = in.Phone
	}

	existingUser.UpdatedAt = time.Now().Unix()

	// 保存更新
	if err := l.svcCtx.UserModel.Update(l.ctx, existingUser); err != nil {
		l.Logger.Errorf("更新用户信息失败: %v", err)
		return nil, errors.New("更新用户信息失败，请稍后重试")
	}

	l.Logger.Infof("用户信息更新成功: userId=%d", in.UserId)

	return &commonpb.BoolResponse{
		Success: true,
		Message: "更新成功",
	}, nil
}
