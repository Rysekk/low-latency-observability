package main

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestParseAggTrade(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		want    AggTrade
		wantErr bool
	}{
		{
			name:    "valid complete json",
			input:   []byte(`{"e":"aggTrade","E":123456789,"s":"BTCUSDT","p":"50000.00","q":"0.5","T":123456790}`),
			want:    AggTrade{EventType: "aggTrade", EventTime: 123456789, Symbol: "BTCUSDT", Price: "50000.00", Quantity: "0.5", TradeTime: 123456790},
			wantErr: false,
		},
		{
			name:    "malformed json",
			input:   []byte(`{"e":"aggTrade","E":123456789,"s":"BTCUSDT"`),
			wantErr: true,
		},
		{
			name:    "wrong type on numeric field",
			input:   []byte(`{"e":"aggTrade","E":123456789,"s":"BTCUSDT","p":"50000.00","q":"0.5","T":"string"}`),
			wantErr: true,
		},
		{
			name:    "empty json",
			input:   []byte(`{}`),
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseAggTrade(tc.input)

			if (err != nil) != tc.wantErr {
				t.Fatalf("parseAggTrade() error = %v, wantErr %v", err, tc.wantErr)
			}

			if tc.wantErr {
				return
			}

			if got != tc.want {
				t.Errorf("parseAggTrade() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestConvertTrade(t *testing.T) {
	tests := []struct {
		name    string
		input   AggTrade
		want    Trade
		wantErr bool
	}{
		{
			name: "Conversion réussie avec valeurs valides",
			input: AggTrade{
				EventType: "aggTrade",
				EventTime: 1672531199000, // 2022-12-31 23:59:59 UTC
				Symbol:    "BTCUSDT",
				Price:     "16800.50",
				Quantity:  "0.015",
				TradeTime: 1672531198000,
			},
			want: Trade{
				EventType: "aggTrade",
				EventTime: time.UnixMilli(1672531199000),
				Symbol:    "BTCUSDT",
				Price:     decimal.RequireFromString("16800.50"),
				Quantity:  decimal.RequireFromString("0.015"),
				TradeTime: time.UnixMilli(1672531198000),
			},
			wantErr: false,
		},
		{
			name: "Wrong Price",
			input: AggTrade{
				EventType: "aggTrade",
				EventTime: 1672531199000, // 2022-12-31 23:59:59 UTC
				Symbol:    "BTCUSDT",
				Price:     "invalide_price",
				Quantity:  "0.015",
				TradeTime: 1672531198000,
			},
			want:    Trade{},
			wantErr: true,
		},
		{
			name: "Check if quantity is not a valid number",
			input: AggTrade{
				EventType: "aggTrade",
				EventTime: 1672531199000, // 2022-12-31 23:59:59 UTC
				Symbol:    "BTCUSDT",
				Price:     "16800.50",
				Quantity:  "abc",
				TradeTime: 1672531198000,
			},
			want:    Trade{},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := convertTrade(tc.input)

			// 1. Check if there is a error
			if (err != nil) != tc.wantErr {
				t.Fatalf("convertTrade() error = %v, expectError %v", err, tc.wantErr)
			}

			// If there is a error, go out of the loop
			if tc.wantErr {
				return
			}

			// 2. Check time and string field
			if got.EventType != tc.want.EventType {
				t.Errorf("EventType: got %q, want %q", got.EventType, tc.want.EventType)
			}
			if !got.EventTime.Equal(tc.want.EventTime) {
				t.Errorf("EventTime: got %v, want %v", got.EventTime, tc.want.EventTime)
			}
			if got.Symbol != tc.want.Symbol {
				t.Errorf("Symbol: got %q, want %q", got.Symbol, tc.want.Symbol)
			}
			if !got.TradeTime.Equal(tc.want.TradeTime) {
				t.Errorf("TradeTime: got %v, want %v", got.TradeTime, tc.want.TradeTime)
			}

			// 3. Check the types decimal.Decimal
			if !got.Price.Equal(tc.want.Price) {
				t.Errorf("Price: got %s, want %s", got.Price, tc.want.Price)
			}
			if !got.Quantity.Equal(tc.want.Quantity) {
				t.Errorf("Quantity: got %s, want %s", got.Quantity, tc.want.Quantity)
			}
		})
	}
}

func BenchmarkChannelBytes(b *testing.B) {
	c := make(chan []byte, 1)
	payload := []byte(`{"e":"aggTrade","E":123456789,"s":"BTCUSDT","p":"50000.00","q":"0.5","T":123456790}`)

	for b.Loop() {
		c <- payload
		<-c
	}
}

func BenchmarkChannelMessage(b *testing.B) {
	c := make(chan message, 1)
	payload := []byte(`{"e":"aggTrade","E":123456789,"s":"BTCUSDT","p":"50000.00","q":"0.5","T":123456790}`)

	for b.Loop() {
		c <- message{payload: payload, readAt: time.Now()}
		<-c
	}
}

func TestLogThrottleAllow(t *testing.T) {
	// A real date, not time.Time{}: the first call must be far from the zero value of last
	start := time.Unix(1000, 0)
	throttle := logThrottle{interval: 10 * time.Second}

	tests := []struct {
		name           string
		now            time.Time
		wantOk         bool
		wantSuppressed int
	}{
		{name: "first error is logged", now: start, wantOk: true, wantSuppressed: 0},
		{name: "inside the interval is suppressed", now: start.Add(1 * time.Second), wantOk: false, wantSuppressed: 0},
		{name: "still inside the interval", now: start.Add(2 * time.Second), wantOk: false, wantSuppressed: 0},
		{name: "after the interval reports the suppressed lines", now: start.Add(10 * time.Second), wantOk: true, wantSuppressed: 2},
		{name: "the interval restarts after a logged line", now: start.Add(11 * time.Second), wantOk: false, wantSuppressed: 0},
		{name: "the suppressed count restarts after a logged line", now: start.Add(20 * time.Second), wantOk: true, wantSuppressed: 1},
	}

	// The calls share one throttle, so the order of the table matters
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ok, suppressed := throttle.allow(tc.now)

			if ok != tc.wantOk {
				t.Errorf("allow() ok = %v, want %v", ok, tc.wantOk)
			}
			if suppressed != tc.wantSuppressed {
				t.Errorf("allow() suppressed = %d, want %d", suppressed, tc.wantSuppressed)
			}
		})
	}
}
