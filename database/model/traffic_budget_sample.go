package model

type TrafficBudgetSample struct {
	Id              uint64 `json:"id" gorm:"primaryKey;autoIncrement"`
	DateTime        int64  `json:"dateTime"`
	PeriodStart     int64  `json:"periodStart"`
	MeteredRxBytes  uint64 `json:"meteredRxBytes"`
	MeteredTxBytes  uint64 `json:"meteredTxBytes"`
	UsedBytes       uint64 `json:"usedBytes"`
	ClientPoolBytes uint64 `json:"clientPoolBytes"`
	Level           string `json:"level"`
}
