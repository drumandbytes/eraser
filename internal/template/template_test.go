package template

import (
	"testing"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/config"
)

func TestSetCustomRemovesTemplate(t *testing.T) {
	e, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetCustom("Hello {{.BrokerName}}"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Render(CustomName, config.Profile{}, broker.Broker{Name: "X"}); err != nil {
		t.Fatal(err)
	}
	if err := e.SetCustom(""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Render(CustomName, config.Profile{}, broker.Broker{}); err == nil {
		t.Error("custom template still renders after removal")
	}
	if err := e.SetCustom("Subject: only a subject\n"); err == nil {
		t.Error("empty body accepted")
	}
}
