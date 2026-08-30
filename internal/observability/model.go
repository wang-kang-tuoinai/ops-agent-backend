package observability

// 定义Log表结构
type LogEntry struct {
	ID       uint64 `gorm:"primaryKey;autoIncrement"`
	Ts       int64  `gorm:"not null;index:idx_svc_level_ts,priority:3;index:idx_route_ts,priority:2;index:idx_template_ts,priority:2"`
	Service  string `gorm:"type:varchar(64);not null;index:idx_svc_level_ts,priority:1"`
	Level    string `gorm:"type:varchar(16);not null;index:idx_svc_level_ts,priority:2"`
	Route    string `gorm:"type:varchar(255);not null;default:'';index:idx_route_ts,priority:1"`
	Template string `gorm:"type:varchar(255);not null;index:idx_template_ts,priority:1"`
	Attrs    string `gorm:"type:json;not null"`
	TraceID  string `gorm:"type:varchar(64);not null;default:''"`
}

// 实现GORM里的Tabel接口，自定义表名
func (LogEntry) TableName() string { return "logs" }
