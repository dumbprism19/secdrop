package ui

import (
	"errors"
	"fmt"

	"github.com/dumbprism19/secdrop/internal/kube"
)

// Pod health comes from the user's current kubectl context and is shown
// inside each service panel. There is no separate screen to learn.

// podsFor lists the pods that run a service.
func (m *Model) podsFor(svc string) []kube.Pod {
	var out []kube.Pod
	for _, p := range m.pods.Pods {
		if p.BelongsTo(svc) {
			out = append(out, p)
		}
	}
	return out
}

// podSection is the panel's pod block: a summary line, then one line per
// unhealthy pod (pods arrive sorted unhealthy first). When there is nothing
// to show it says why, so the feature is never silently missing.
func (m *Model) podSection(svc string, w int) []string {
	ps := m.podsFor(svc)
	label := sMuted.Render("Pods  ")
	if len(ps) == 0 {
		switch {
		case m.podsBusy && m.podsAt.IsZero():
			return []string{label + m.spin.View() + sFaint.Render(" checking kubectl…")}
		case errors.Is(m.podsErr, kube.ErrGcloudAuth):
			return []string{label + sFaint.Render("kubectl's gcloud sign-in expired · ") + sKey.Render(paletteKey) + sFaint.Render(" → Sign in again")}
		case m.podsErr != nil:
			return []string{label + sFaint.Render(trunc("unavailable: "+m.podsErr.Error(), w-6))}
		case !m.podsAt.IsZero():
			return []string{label + sFaint.Render("none running for "+svc+" in "+trunc(m.pods.Context, max(10, w/2)))}
		}
		return nil
	}
	bad := 0
	for _, p := range ps {
		if !p.Healthy() {
			bad++
		}
	}
	ctx := sFaint.Render("  ·  " + trunc(m.pods.Context, max(10, w/3)))
	if bad == 0 {
		return []string{label + sOK.Render("●") + sText.Render(" all "+fmt.Sprint(len(ps))+" healthy") + ctx}
	}
	out := []string{label + sHot.Render(fmt.Sprintf("▲ %d of %d unhealthy", bad, len(ps))) + ctx}
	shown := 0
	for _, p := range ps {
		if p.Healthy() || shown == 3 {
			continue
		}
		shown++
		detail := fmt.Sprintf("%d/%d ready · %s", p.Ready, p.Total, plural(p.Restarts, "restart"))
		out = append(out, trunc("      "+sHot.Render(pad(trunc(p.Status(), 18), 19))+sText.Render(trunc(p.Name, w/2))+"  "+sMuted.Render(detail), w))
	}
	if bad > shown {
		out = append(out, "      "+sFaint.Render(fmt.Sprintf("and %d more · %s → pod name", bad-shown, paletteKey)))
	}
	return out
}
