package worktime

import "time"

// roundTime 将 t 按 unit 分钟就近取整，恰在正中间时向上取。
// 5、6、15 分钟均整除 24 小时，故按绝对时间取整与按当日零点取整一致。
func roundTime(t time.Time, unit int) time.Time {
	return t.Round(time.Duration(unit) * time.Minute)
}

// effectiveIn 计算上班打卡的计薪开始时刻：
// 晚于排班开始不超过宽限分钟数的，按排班开始时间计，不再取整；
// 否则按取整后的打卡时间计。
func effectiveIn(p Punch, cfg Config) time.Time {
	if p.ScheduledStart != nil {
		sched := *p.ScheduledStart
		if p.Time.After(sched) && !p.Time.After(sched.Add(time.Duration(cfg.GraceMinutes)*time.Minute)) {
			return sched
		}
	}
	return roundTime(p.Time, cfg.RoundMinutes)
}
