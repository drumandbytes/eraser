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

func TestUseConfigAndRenderErrors(t *testing.T) {
	e, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	if err := e.UseConfig(nil); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Options: config.Options{CustomTemplate: "Hi {{.BrokerName}}"}}
	if err := e.UseConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Render(CustomName, config.Profile{}, broker.Broker{Name: "X"}); err != nil {
		t.Fatal(err)
	}
	cfg.Options.CustomTemplate = "{{index .FullName 99}}"
	if err := e.UseConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Render(CustomName, config.Profile{}, broker.Broker{}); err == nil {
		t.Error("runtime template error not reported")
	}
	if err := e.SetCustom("{{.Unclosed"); err == nil {
		t.Error("syntax error accepted")
	}
}

func TestCheckCustom(t *testing.T) {
	if err := CheckCustom("Hi {{.FullName}}"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "Hi {{.Nope}}", "{{.Unclosed"} {
		if err := CheckCustom(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
