package engine

import (
	"context"
	"encoding/json"
	"testing"
)

func TestTrafficRatesWithoutProfileStore(t *testing.T) {
	e := &Engine{}
	value, err := e.Call(context.Background(), "engine.traffic", nil)
	if err != nil {
		t.Fatal(err)
	}
	rates := value.(TrafficRates)
	if rates.Up != 0 || rates.Down != 0 || rates.UpTotal != 0 || rates.DownTotal != 0 || rates.At == 0 {
		t.Fatalf("idle counters: %+v", rates)
	}
	body, _ := json.Marshal(rates)
	var fields map[string]any
	_ = json.Unmarshal(body, &fields)
	if len(fields) != 5 {
		t.Fatalf("lightweight endpoint includes slow statistics: %s", body)
	}
	if _, err = e.Call(context.Background(), "engine.traffic", json.RawMessage(`{"unexpected":true}`)); err == nil {
		t.Fatal("accepted unknown traffic parameters")
	}
	e.closed = true
	if _, err = e.Call(context.Background(), "engine.traffic", nil); asError(err).Code != "closed" {
		t.Fatalf("closed traffic query: %v", err)
	}
}
