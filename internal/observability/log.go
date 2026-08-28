package observability

const (
	LevelDebug = "DEBUG"
	LevelInfo  = "INFO"
	LevelWarn  = "WARN"
	LevelError = "ERROR"
)

// 定义日志Template,方便做日志聚合
const (
	// 错误路径（在 HandleError 里统一记录）
	TplUserNotFound  = "user not found"
	TplDuplicateUser = "duplicate user creation attempt"
	TplLockConflict  = "distributed lock conflict"
	TplBadRequest    = "invalid request parameter"
	TplInternalError = "internal server error"

	// 非错误的观测点（在发生处记录）
	TplCacheMiss       = "cache miss for user {user_id}"
	TplBloomBlocked    = "request blocked by bloom filter for {id}"
	TplPublishFailed   = "user register event publish failed for user {user_id}"
	TplUnlockFailed    = "distributed lock release failed for {lock_key}"
)
