package logic

import (
	"context"
	"errors"

	"seckill-mall/user-service/internal/model"
	"seckill-mall/user-service/internal/svc"
	"seckill-mall/user-service/user"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetUserInfoLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetUserInfoLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserInfoLogic {
	return &GetUserInfoLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetUserInfo 获取用户信息
func (l *GetUserInfoLogic) GetUserInfo(in *user.GetUserInfoRequest) (*user.UserInfo, error) {
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

	return &user.UserInfo{
		Id:        existingUser.ID,
		Username:  existingUser.Username,
		Email:     existingUser.Email,
		Phone:     existingUser.Phone,
		Status:    existingUser.Status,
		CreatedAt: existingUser.CreatedAt,
		UpdatedAt: existingUser.UpdatedAt,
	}, nil
}
