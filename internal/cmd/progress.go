package cmd

import (
	"fmt"
	"io"
	"time"

	"github.com/webkaz-labs/updev/internal/i18n"
)

const (
	startupProgressDelay    = 150 * time.Millisecond
	startupProgressInterval = 180 * time.Millisecond
)

type startupProgress struct {
	enabled bool
	w       io.Writer
	message string
	delay   time.Duration
	stop    chan struct{}
	done    chan struct{}
}

func newStartupProgress(input io.Reader, w io.Writer, format string, message string) startupProgress {
	return startupProgress{
		enabled: shouldShowStartupProgress(input, w, format),
		w:       w,
		message: message,
		delay:   startupProgressDelay,
	}
}

func shouldShowStartupProgress(input io.Reader, w io.Writer, format string) bool {
	if value, ok := boolEnv("UPDEV_PROGRESS"); ok {
		if !value {
			return false
		}
	} else if configured := loadUpdevConfig().UI.Progress; configured != nil && !*configured {
		return false
	}
	if format != "text" {
		return false
	}
	return isTerminal(input) && isTerminal(w)
}

func (p *startupProgress) Start() {
	if !p.enabled || p.w == nil || p.message == "" || p.stop != nil {
		return
	}
	p.stop = make(chan struct{})
	p.done = make(chan struct{})
	go p.run()
}

func (p *startupProgress) Done() {
	if !p.enabled || p.stop == nil {
		return
	}
	close(p.stop)
	<-p.done
	p.stop = nil
	p.done = nil
}

func (p *startupProgress) run() {
	rendered := false
	defer close(p.done)
	if p.delay > 0 {
		timer := time.NewTimer(p.delay)
		select {
		case <-p.stop:
			timer.Stop()
			return
		case <-timer.C:
		}
	}
	startedAt := time.Now()
	frames := []string{"-", "\\", "|", "/"}
	frame := 0
	renderProgressFrame(p.w, frames[frame], p.message, 0)
	rendered = true
	ticker := time.NewTicker(startupProgressInterval)
	defer ticker.Stop()
	for {
		select {
		case <-p.stop:
			if rendered {
				clearProgressLine(p.w)
			}
			return
		case <-ticker.C:
			frame = (frame + 1) % len(frames)
			renderProgressFrame(p.w, frames[frame], p.message, time.Since(startedAt))
		}
	}
}

func renderProgressFrame(w io.Writer, frame string, message string, elapsed time.Duration) {
	suffix := ""
	if elapsed >= time.Second {
		suffix = fmt.Sprintf(" (%ds)", int(elapsed/time.Second))
	}
	fmt.Fprintf(w, "\r%s %s%s", frame, message, suffix)
}

func clearProgressLine(w io.Writer) {
	fmt.Fprint(w, "\r\033[2K")
}

func inventoryProgressMessage(lang string, refresh bool) string {
	if refresh {
		return i18n.Pick(lang,
			"Refreshing package inventory...",
			"パッケージ inventory を更新中...",
		)
	}
	return i18n.Pick(lang,
		"Loading package inventory...",
		"パッケージ inventory を読み込み中...",
	)
}

func safetyProgressMessage(lang string) string {
	return i18n.Pick(lang,
		"Checking update safety gates...",
		"更新前の安全性を確認中...",
	)
}

func descriptionTranslationProgressMessage(lang string) string {
	return i18n.Pick(lang,
		"Refreshing translated descriptions...",
		"説明の翻訳 cache を更新中...",
	)
}

func reviewPlanProgressMessage(lang string) string {
	return i18n.Pick(lang,
		"Preparing review actions...",
		"確認アクションを準備中...",
	)
}

func securityScanProgressMessage(lang string) string {
	return i18n.Pick(lang,
		"Collecting security evidence...",
		"セキュリティ証跡を収集中...",
	)
}

func securityReviewProgressMessage(lang string) string {
	return i18n.Pick(lang,
		"Preparing security review candidates...",
		"セキュリティ review 候補を準備中...",
	)
}

func syncProgressMessage(lang string, refresh bool) string {
	if refresh {
		return i18n.Pick(lang,
			"Refreshing sync inventory...",
			"sync inventory を更新中...",
		)
	}
	return i18n.Pick(lang,
		"Loading sync inventory...",
		"sync inventory を読み込み中...",
	)
}

func mutationProgressMessage(lang string, action string) string {
	return i18n.Pick(lang,
		"Applying "+action+" and validating inventory...",
		action+" を適用して inventory を検証中...",
	)
}
