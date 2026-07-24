package archive

import (
	"testing"
	"time"
)

func TestRenderPathDefaultTemplate(t *testing.T) {
	tm := time.Date(2005, 6, 15, 0, 0, 0, 0, time.UTC)
	got, err := RenderPath(DefaultPathTemplate, tm)
	if err != nil {
		t.Fatalf("RenderPath: %v", err)
	}
	want := "2005/2005-06"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRenderPathWithDayToken(t *testing.T) {
	tm := time.Date(2005, 6, 5, 0, 0, 0, 0, time.UTC)
	got, err := RenderPath("{year}/{month}/{day}", tm)
	if err != nil {
		t.Fatalf("RenderPath: %v", err)
	}
	if got != "2005/06/05" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderPathUnknownToken(t *testing.T) {
	_, err := RenderPath("{year}/{bogus}", time.Now())
	if err == nil {
		t.Fatal("expected error for unknown token")
	}
}

func TestRenderPathEmptyTemplate(t *testing.T) {
	_, err := RenderPath("", time.Now())
	if err == nil {
		t.Fatal("expected error for empty template")
	}
}
