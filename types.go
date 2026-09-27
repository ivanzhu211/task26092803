// Package worktime 按工会谈判单位规则对打卡记录进行工时分类计算。
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

// Punch 一条打卡记录。Time 为本地时间，精确到秒，不考虑夏令时。
type Punch struct {
	ID         string
	EmployeeID string
	Type       PunchType
	Time       time.Time
	// SchedStart 仅上班打卡可附带的排班开始时间。
	SchedStart time.Time
	HasSched   bool
}

// Correction 更正记录：指向原记录号，给出新时间或作废。
type Correction struct {
	TargetID    string
	SubmittedAt time.Time
	Void        bool
	NewTime     time.Time
}

// Config 工会谈判单位的规则配置。
type Config struct {
	// RoundingMinutes 取整单位，仅允许 5、6、15。
	RoundingMinutes int
	// DailyOTEnabled 每日超过 8 小时记加班规则；关闭后双倍规则照常执行。
	DailyOTEnabled bool
}

// Category 工时类别。
type Category int

const (
	Regular Category = iota
	Overtime
	Double
)

// Segment 一段已分类的工时，附来源打卡记录号。
type Segment struct {
	Start     time.Time
	End       time.Time
	Category  Category
	RecordIDs []string
}

// AnomalyReason 异常原因。
type AnomalyReason string

const (
	ReasonMissingOut   AnomalyReason = "missing_out"
	ReasonMissingIn    AnomalyReason = "missing_in"
	ReasonShiftTooLong AnomalyReason = "shift_too_long"
	ReasonUnpairedMeal AnomalyReason = "unpaired_meal"
	ReasonMealOutside  AnomalyReason = "meal_outside_shift"
)

// Anomaly 一条异常，附相关打卡记录号。
type Anomaly struct {
	Reason    AnomalyReason
	RecordIDs []string
}

// WeekResult 某员工某周的分类结果。WeekStart 为周日 00:00。
type WeekResult struct {
	WeekStart       time.Time
	RegularMinutes  int
	OvertimeMinutes int
	DoubleMinutes   int
	Segments        []Segment
}

// EmployeeResult 某员工的完整结果。
type EmployeeResult struct {
	EmployeeID string
	Weeks      []WeekResult
	Anomalies  []Anomaly
}

// Input 一次计算的输入。
type Input struct {
	Config      Config
	Punches     []Punch
	Corrections []Correction
}
