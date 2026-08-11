package evaluation

import "math"

// CampaignStorageLimitBytes is a hard cap for recordings newly created by a
// campaign. Existing recordings are deliberately outside this accounting.
const CampaignStorageLimitBytes int64 = 200 * 1024 * 1024 * 1024

// StorageForecast combines the persisted baseline with measurements from the
// fresh full-universe recorder session. It is intentionally conservative: an
// unknown or overflowing estimate cannot admit shadow work.
type StorageForecast struct {
	BaselineBytes        int64
	RecordedBytes        int64
	MeasuredBytesPerHour int64
	ShadowHours          int64
	SafetyBufferBytes    int64
}

// ProjectedBytes returns the final campaign recording footprint, including a
// required reserve for the complete seven-day shadow session.
func (value StorageForecast) ProjectedBytes() (int64, bool) {
	if value.BaselineBytes < 0 || value.RecordedBytes < 0 || value.MeasuredBytesPerHour < 0 ||
		value.ShadowHours < 0 || value.SafetyBufferBytes < 0 {
		return 0, false
	}
	if value.MeasuredBytesPerHour != 0 && value.ShadowHours > (math.MaxInt64-value.SafetyBufferBytes-value.RecordedBytes)/value.MeasuredBytesPerHour {
		return 0, false
	}
	projected := value.RecordedBytes + value.MeasuredBytesPerHour*value.ShadowHours
	if projected > math.MaxInt64-value.SafetyBufferBytes {
		return 0, false
	}
	return projected + value.SafetyBufferBytes, true
}

// ShadowAdmissible protects the complete shadow reserve before it starts.
func (value StorageForecast) ShadowAdmissible() bool {
	projected, ok := value.ProjectedBytes()
	return ok && projected <= CampaignStorageLimitBytes
}
