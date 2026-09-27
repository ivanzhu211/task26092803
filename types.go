package worktime

import "time"

// PunchType 打卡类型。
type PunchType int

const (
	PunchIn PunchType = iota
	MealStart
	MealEnd
	PunchOut
)

// Punch 一条打卡记录。Time 为本地时间，精确到秒。
// ScheduledStart 仅上班打卡可附带，表示排班开始时间。
type Punch struct {
	ID             string
	EmployeeID     string
	Type           PunchType
	Time           time.Time
	ScheduledStart *time.Time
}

// Correction 更正记录：指向原记录号，给出新时间或作废。
// 同一原记录多次更正时，以 SubmittedAt 最晚者为准。
type Correction struct {
	TargetID    string
	SubmittedAt time.Time
	NewTime     *time.Time // 非空表示改时间
	Void        bool       // 为 true 表示作废原记录
}

// Config 工会谈判单位的规则配置。
type Config struct {
	RoundMinutes    int  // 取整单位：5、6 或 15 分钟
	GraceMinutes    int  // 上班晚于排班开始的宽限分钟数（如 7）
	DisableDailyOT8 bool // 关闭"每日超 8 小时记加班"规则
}

// Segment 一段工时，起止时刻均为本地时间，附来源打卡记录号。
type Segment struct {
	Start     time.Time
	End       time.Time
	Minutes   int
	SourceIDs []string
}

// Anomaly 异常打卡说明，附相关记录号。
type Anomaly struct {
	Reason    string
	RecordIDs []string
	at        time.Time // 归属判定用，不导出
}

// WeekResult 某员工某一周（周日 0 点起）的分类结果。
type WeekResult struct {
	WeekStart time.Time
	Regular   []Segment
	Overtime  []Segment
	Double    []Segment
	Anomalies []Anomaly
}

// EmployeeResult 某员工的全部周结果。
type EmployeeResult struct {
	EmployeeID string
	Weeks      []WeekResult
}
