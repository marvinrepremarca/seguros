package kernel_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

func date(t *testing.T, s string) kernel.Date {
	t.Helper()
	d, err := kernel.ParseDate(s)
	require.NoError(t, err)
	return d
}

func TestNewDate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		y       int
		m       time.Month
		d       int
		wantErr bool
	}{
		{2026, time.October, 30, false},
		{2028, time.February, 29, false},
		{2026, time.February, 29, true},
		{2026, time.February, 30, true},
		{2026, 13, 1, true},
		{2026, time.January, 0, true},
		{0, time.January, 1, true},
		{10000, time.January, 1, true},
	}
	for _, tc := range tests {
		d, err := kernel.NewDate(tc.y, tc.m, tc.d)
		if tc.wantErr {
			require.ErrorIs(t, err, kernel.ErrInvalidDate, "%d-%d-%d", tc.y, tc.m, tc.d)
			continue
		}
		require.NoError(t, err)
		assert.Equal(t, tc.y, d.Year())
		assert.Equal(t, tc.m, d.Month())
		assert.Equal(t, tc.d, d.Day())
	}
}

func TestParseDate(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "2026-10-30", date(t, "2026-10-30").String())
	for _, bad := range []string{"30/10/2026", "2026-10-30T00:00:00Z", "2026-02-30", ""} {
		_, err := kernel.ParseDate(bad)
		require.ErrorIs(t, err, kernel.ErrInvalidDate, bad)
	}
}

func TestDateOfAndToday(t *testing.T) {
	t.Parallel()
	// 2026-10-31 03:00 UTC = 2026-10-30 22:00 en Bogotá.
	instant := time.Date(2026, 10, 31, 3, 0, 0, 0, time.UTC)
	assert.Equal(t, "2026-10-31", kernel.DateOf(instant, time.UTC).String())
	assert.Equal(t, "2026-10-30", kernel.DateOf(instant, kernel.BusinessLocation()).String())
	assert.Equal(t, "2026-10-30", kernel.Today(kernel.NewFakeClock(instant)).String())

	_, offset := instant.In(kernel.BusinessLocation()).Zone()
	assert.Equal(t, -5*3600, offset)
	assert.Equal(t, "America/Bogota", kernel.BusinessLocation().String())
}

func TestDateComparisonAndArithmetic(t *testing.T) {
	t.Parallel()
	a, b := date(t, "2026-12-31"), date(t, "2027-01-01")
	assert.True(t, a.Before(b))
	assert.True(t, b.After(a))
	assert.False(t, a.After(a))
	assert.Equal(t, 0, a.Compare(date(t, "2026-12-31")))
	assert.Equal(t, -1, a.Compare(b))
	assert.Equal(t, b, a.AddDays(1))
	assert.Equal(t, date(t, "2028-02-29"), date(t, "2028-03-01").AddDays(-1))
	assert.Equal(t, 365, date(t, "2026-01-01").DaysUntil(date(t, "2027-01-01")))
	assert.Equal(t, 366, date(t, "2028-01-01").DaysUntil(date(t, "2029-01-01")))
	assert.Equal(t, -1, b.DaysUntil(a))

	start := a.StartOf(kernel.BusinessLocation())
	assert.Equal(t, time.Date(2026, 12, 31, 5, 0, 0, 0, time.UTC), start.UTC())
}

func TestDateZero(t *testing.T) {
	t.Parallel()
	var z kernel.Date
	assert.True(t, z.IsZero())
	assert.Empty(t, z.String())
	assert.False(t, date(t, "2026-01-01").IsZero())
}

func TestDateJSON(t *testing.T) {
	t.Parallel()
	type dto struct {
		From kernel.Date `json:"from"`
		To   kernel.Date `json:"to"`
	}
	b, err := json.Marshal(dto{From: date(t, "2026-10-30")})
	require.NoError(t, err)
	assert.JSONEq(t, `{"from":"2026-10-30","to":""}`, string(b))

	var got dto
	require.NoError(t, json.Unmarshal([]byte(`{"from":"2027-01-15","to":""}`), &got))
	assert.Equal(t, date(t, "2027-01-15"), got.From)
	assert.True(t, got.To.IsZero())

	require.Error(t, json.Unmarshal([]byte(`{"from":"15/01/2027"}`), &got))
}

func TestDateSQL(t *testing.T) {
	t.Parallel()
	d := date(t, "2026-10-30")
	v, err := d.Value()
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC), v)

	v, err = kernel.Date{}.Value()
	require.NoError(t, err)
	assert.Nil(t, v)

	tests := []struct {
		name string
		src  any
		want kernel.Date
	}{
		{"time", time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC), d},
		{"string", "2026-10-30", d},
		{"bytes", []byte("2026-10-30"), d},
		{"nil", nil, kernel.Date{}},
	}
	for _, tc := range tests {
		var got kernel.Date
		require.NoError(t, got.Scan(tc.src), tc.name)
		assert.Equal(t, tc.want, got, tc.name)
	}
	var got kernel.Date
	require.Error(t, got.Scan(42))
	require.ErrorIs(t, got.Scan("2026-02-30"), kernel.ErrInvalidDate)
}

func TestDateRange(t *testing.T) {
	t.Parallel()
	from, to := date(t, "2026-01-01"), date(t, "2027-01-01")
	r, err := kernel.NewDateRange(from, to)
	require.NoError(t, err)
	assert.Equal(t, from, r.From())
	assert.Equal(t, to, r.To())
	assert.Equal(t, 365, r.Days())
	assert.True(t, r.Contains(from))
	assert.True(t, r.Contains(to))
	assert.True(t, r.Contains(date(t, "2026-06-15")))
	assert.False(t, r.Contains(date(t, "2025-12-31")))
	assert.False(t, r.Contains(date(t, "2027-01-02")))

	for _, tc := range []struct {
		name     string
		from, to kernel.Date
	}{
		{"fin igual al inicio", from, from},
		{"fin anterior", to, from},
		{"inicio vacío", kernel.Date{}, to},
		{"fin vacío", from, kernel.Date{}},
	} {
		_, err := kernel.NewDateRange(tc.from, tc.to)
		require.ErrorIs(t, err, kernel.ErrInvalidDateRange, tc.name)
	}
}
