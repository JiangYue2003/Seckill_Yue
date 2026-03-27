package errors

import (
	"fmt"
	"net/http"
)

// ============================================================
// 自定义错误类型定义
// ============================================================

// ErrCode 业务错误码
type ErrCode int

const (
	// 通用错误码 (1xxx)
	CodeSuccess            ErrCode = 0    // 成功
	CodeInvalidParams      ErrCode = 1001 // 参数错误
	CodeUnauthorized       ErrCode = 1002 // 未授权
	CodeForbidden          ErrCode = 1003 // 禁止访问
	CodeNotFound           ErrCode = 1004 // 资源不存在
	CodeInternalError      ErrCode = 1005 // 内部错误
	CodeServiceUnavailable ErrCode = 1006 // 服务不可用
	CodeTimeout            ErrCode = 1007 // 超时
	CodeTooManyRequests    ErrCode = 1008 // 请求过于频繁

	// 用户服务错误码 (2xxx)
	CodeUserNotFound       ErrCode = 2001 // 用户不存在
	CodeUserAlreadyExists  ErrCode = 2002 // 用户已存在
	CodeInvalidPassword    ErrCode = 2003 // 密码错误
	CodeInvalidToken       ErrCode = 2004 // Token 无效
	CodeTokenExpired       ErrCode = 2005 // Token 已过期
	CodeInvalidOldPassword ErrCode = 2006 // 旧密码错误

	// 商品服务错误码 (3xxx)
	CodeProductNotFound   ErrCode = 3001 // 商品不存在
	CodeProductOffShelf   ErrCode = 3002 // 商品已下架
	CodeStockNotEnough    ErrCode = 3003 // 库存不足
	CodeStockDeductFailed ErrCode = 3004 // 库存扣减失败

	// 秒杀服务错误码 (4xxx)
	CodeSeckillNotStarted     ErrCode = 4001 // 秒杀未开始
	CodeSeckillEnded          ErrCode = 4002 // 秒杀已结束
	CodeSeckillSoldOut        ErrCode = 4003 // 秒杀已售罄
	CodeSeckillAlreadyBuy     ErrCode = 4004 // 已购买过该秒杀商品
	CodeSeckillPerLimit       ErrCode = 4005 // 超出限购数量
	CodeSeckillInvalidRequest ErrCode = 4006 // 秒杀请求无效

	// 订单服务错误码 (5xxx)
	CodeOrderNotFound         ErrCode = 5001 // 订单不存在
	CodeOrderAlreadyPaid      ErrCode = 5002 // 订单已支付
	CodeOrderAlreadyCancelled ErrCode = 5003 // 订单已取消
	CodeOrderCannotRefund     ErrCode = 5004 // 订单不可退款
	CodeOrderStatusInvalid    ErrCode = 5005 // 订单状态不正确
)

// ============================================================
// 错误结构体
// ============================================================

type BizError struct {
	Code     ErrCode `json:"code"`
	Message  string  `json:"message"`
	Cause    error   `json:"-"` // 内部错误，不对外暴露
	HttpCode int     `json:"-"`
}

func (e *BizError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%d] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%d] %s", e.Code, e.Message)
}

func (e *BizError) Unwrap() error {
	return e.Cause
}

// NewBizError 创建业务错误
func NewBizError(code ErrCode, message string) *BizError {
	return &BizError{
		Code:     code,
		Message:  message,
		HttpCode: MapCodeToHttpStatus(code),
	}
}

// NewBizErrorWithCause 创建带内部错误原因的业务错误
func NewBizErrorWithCause(code ErrCode, message string, cause error) *BizError {
	return &BizError{
		Code:     code,
		Message:  message,
		Cause:    cause,
		HttpCode: MapCodeToHttpStatus(code),
	}
}

// ============================================================
// 预定义的错误实例（避免重复创建）
// ============================================================

var (
	ErrSuccess            = NewBizError(CodeSuccess, "操作成功")
	ErrInvalidParams      = NewBizError(CodeInvalidParams, "参数错误")
	ErrUnauthorized       = NewBizError(CodeUnauthorized, "未授权，请先登录")
	ErrForbidden          = NewBizError(CodeForbidden, "禁止访问")
	ErrNotFound           = NewBizError(CodeNotFound, "资源不存在")
	ErrInternal           = NewBizError(CodeInternalError, "内部服务器错误")
	ErrServiceUnavailable = NewBizError(CodeServiceUnavailable, "服务暂不可用")
	ErrTimeout            = NewBizError(CodeTimeout, "请求超时")
	ErrTooManyRequests    = NewBizError(CodeTooManyRequests, "请求过于频繁，请稍后再试")

	// 用户服务
	ErrUserNotFound       = NewBizError(CodeUserNotFound, "用户不存在")
	ErrUserAlreadyExists  = NewBizError(CodeUserAlreadyExists, "用户已存在")
	ErrInvalidPassword    = NewBizError(CodeInvalidPassword, "密码错误")
	ErrInvalidToken       = NewBizError(CodeInvalidToken, "Token 无效")
	ErrTokenExpired       = NewBizError(CodeTokenExpired, "Token 已过期")
	ErrInvalidOldPassword = NewBizError(CodeInvalidOldPassword, "旧密码错误")

	// 商品服务
	ErrProductNotFound   = NewBizError(CodeProductNotFound, "商品不存在")
	ErrProductOffShelf   = NewBizError(CodeProductOffShelf, "商品已下架")
	ErrStockNotEnough    = NewBizError(CodeStockNotEnough, "库存不足")
	ErrStockDeductFailed = NewBizError(CodeStockDeductFailed, "库存扣减失败")

	// 秒杀服务
	ErrSeckillNotStarted = NewBizError(CodeSeckillNotStarted, "秒杀活动未开始")
	ErrSeckillEnded      = NewBizError(CodeSeckillEnded, "秒杀活动已结束")
	ErrSeckillSoldOut    = NewBizError(CodeSeckillSoldOut, "商品已售罄")
	ErrSeckillAlreadyBuy = NewBizError(CodeSeckillAlreadyBuy, "您已购买过该商品")
	ErrSeckillPerLimit   = NewBizError(CodeSeckillPerLimit, "购买数量超出限制")
	ErrSeckillInvalidReq = NewBizError(CodeSeckillInvalidRequest, "秒杀请求无效")

	// 订单服务
	ErrOrderNotFound         = NewBizError(CodeOrderNotFound, "订单不存在")
	ErrOrderAlreadyPaid      = NewBizError(CodeOrderAlreadyPaid, "订单已支付")
	ErrOrderAlreadyCancelled = NewBizError(CodeOrderAlreadyCancelled, "订单已取消")
	ErrOrderCannotRefund     = NewBizError(CodeOrderCannotRefund, "订单状态不支持退款")
	ErrOrderStatusInvalid    = NewBizError(CodeOrderStatusInvalid, "订单状态不正确")
)

// ============================================================
// HTTP Status 映射
// ============================================================

func MapCodeToHttpStatus(code ErrCode) int {
	switch {
	case code == CodeSuccess:
		return http.StatusOK
	case code >= 1001 && code <= 1099:
		return http.StatusBadRequest
	case code == CodeUnauthorized || code == CodeInvalidToken || code == CodeTokenExpired:
		return http.StatusUnauthorized
	case code == CodeForbidden:
		return http.StatusForbidden
	case code == CodeNotFound || code == CodeUserNotFound || code == CodeProductNotFound || code == CodeOrderNotFound:
		return http.StatusNotFound
	case code == CodeTooManyRequests:
		return http.StatusTooManyRequests
	case code >= 5001 && code <= 5999:
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

// ============================================================
// 便捷构造函数
// ============================================================

// InvalidParams 创建参数错误
func InvalidParams(v interface{}) *BizError {
	return NewBizError(CodeInvalidParams, fmt.Sprintf("参数错误: %v", v))
}

// InternalError 创建内部错误
func InternalError(err error) *BizError {
	return NewBizErrorWithCause(CodeInternalError, "内部服务器错误", err)
}

// UserError 创建用户相关错误
func UserError(code ErrCode, msg string) *BizError {
	return NewBizError(code, msg)
}

// ProductError 创建商品相关错误
func ProductError(code ErrCode, msg string) *BizError {
	return NewBizError(code, msg)
}

// SeckillError 创建秒杀相关错误
func SeckillError(code ErrCode, msg string) *BizError {
	return NewBizError(code, msg)
}

// OrderError 创建订单相关错误
func OrderError(code ErrCode, msg string) *BizError {
	return NewBizError(code, msg)
}
