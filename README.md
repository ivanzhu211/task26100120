# task26100120

Go 设施停车计费库：单次停车的分时费率、会员折扣与本地自然日封顶。

## 使用

```go
plan, err := parking.CreatePlan("America/New_York", []parking.Band{
    {StartMin: 0, CentsPerHour: 600},   // 00:00 起 600 分/小时
    {StartMin: 90, CentsPerHour: 1200}, // 01:30 起 1200 分/小时
    {StartMin: 120, CentsPerHour: 600}, // 02:00 起 600 分/小时
}, 8000) // 按原价 80% 收费
if err != nil { /* ... */ }

q := plan.Quote(enterUTC, exitUTC) // 半开区间 [enter, exit)，精确到秒
for _, d := range q.Days {
    fmt.Println(d.Date, d.Seconds, d.Amount) // 本地日期、各档秒数、当日金额(分)
}
fmt.Println(q.Total) // 总金额(分)，为各日已取整金额之和
```

- 费率档按本地日内起始分钟严格递增，首档从 0 开始，最多 48 档；每天共用同一方案。
- 计费跟随真实时刻的本地钟面：回拨重复的区间两次计费，前跳跳过的区间不收费；
  被整日跳过的本地日期（如 Pacific/Apia 的 2011-12-30）不产生账单。
- 每日金额 = 各档秒数×费率求和 ÷3600 ×折扣，按 2500 分封顶后四舍五入（半分向上）；
  全程整数运算，封顶不跨停车记录累计。

## 测试

```sh
go test ./...
```
