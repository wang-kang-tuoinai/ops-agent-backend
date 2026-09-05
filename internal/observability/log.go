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
	// 内部错误，按出错组件细分（未分类的落到 TplInternalError）
	TplInternalError      = "internal server error"
	TplInternalErrorMySQL = "internal server error in mysql"
	TplInternalErrorCache = "internal server error in cache"
	TplInternalErrorMQ    = "internal server error in mq"
	// 在发生处记录（不往上传，或被降级掩盖的事件）
	TplCacheMiss        = "cache miss for user {user_id}"
	TplCacheStoreFailed = "failed to store cache for user {user_id}"
	TplCacheReadFailed  = "failed to read cache for user {user_id}"
	TplCacheDelFailed   = "failed to delete cache for user {user_id}"
	TplBloomBlocked     = "request blocked by bloom filter for {id}"
	TplPublishFailed    = "user register event publish failed for user {user_id}"
	TplUnlockFailed     = "distributed lock release failed for {lock_key}"
)
