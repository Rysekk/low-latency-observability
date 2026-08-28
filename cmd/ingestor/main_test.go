package main

import "testing"

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
			wantErr: true,
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
