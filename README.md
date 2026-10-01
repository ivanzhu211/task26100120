# task26100120

## parking — 设施停车计费库

Go 库：单次停车的分时费率、会员折扣、本地自然日封顶。正确处理夏令时回拨（重复钟面两次计费）、前跳（跳过区间不收费）与整日跳过（如 Pacific/Apia 2011-12-30）。

### 使用

```go
p, err := parking.CreatePlan("America/New_York", []parking.Band{
    {StartMin: 0,   RatePerHour: 600},  // 00:00 起 600 分/小时
    {StartMin: 90,  RatePerHour: 1200}, // 01:30 起
    {StartMin: 120, RatePerHour: 600},  // 02:00 起至当日结束
}, 8000) // 会员折扣：按原价 80% 收费

q, err := parking.Quote(p, enterUTC, exitUTC) // 半开区间 [enter, exit)
// q.Days：按本地日期排序的日账单（Date、各费率档秒数 Bands、每日金额 Amount）
// q.Total：总金额（各日取整后金额之和）
```

- 费率档按本地日内起始分钟严格递增，首档必须从 0 开始，最多 48 档。
- 每日金额 = Σ(秒数 × 小时费率) / 3600 × 折扣，封顶 2500 分后四舍五入（半分向上）。
- 封顶按本地自然日分别计算，不跨停车记录累计。

### 测试

```sh
go test ./...
```
