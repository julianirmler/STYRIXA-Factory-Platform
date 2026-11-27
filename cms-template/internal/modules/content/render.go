package content

import (
	"html"
	"html/template"
	"strconv"
	"strings"
)

// FAQItem ist eine Frage mit Antwort als reiner Text – Grundlage für
// FAQPage-JSON-LD (Ausgabe übernimmt in Phase 4 das SEO-Modul).
type FAQItem struct {
	Question string
	Answer   string
}

// Rendered ist eine gerenderte Seite.
type Rendered struct {
	HTML template.HTML
	FAQ  []FAQItem // aus faq- und answer-Blöcken, in Reihenfolge
}

// Render rendert einen gespeicherten Body als HTML.
func Render(body, format string) (template.HTML, error) {
	r, err := RenderPage(body, format)
	return r.HTML, err
}

// RenderPage rendert einen gespeicherten Body und sammelt die FAQ-Daten.
// Format html (nur von Admins speicherbar) wird ungefiltert ausgegeben.
func RenderPage(body, format string) (Rendered, error) {
	if format == FormatHTML {
		return Rendered{HTML: template.HTML(body)}, nil
	}
	if format != FormatBlocks {
		return Rendered{}, badInput("Unbekanntes Format %q.", format)
	}
	doc, err := parseDoc(body)
	if err != nil {
		return Rendered{}, err
	}
	r := renderer{ids: anchors{}}
	for _, b := range doc.Blocks {
		v, err := decodeBlock(b)
		if err != nil {
			return Rendered{}, err
		}
		r.block(v)
	}
	return Rendered{HTML: template.HTML(r.out.String()), FAQ: r.faq}, nil
}

type renderer struct {
	out strings.Builder
	ids anchors
	faq []FAQItem
}

func (r *renderer) w(s ...string) {
	for _, x := range s {
		r.out.WriteString(x)
	}
}

func (r *renderer) block(v any) {
	switch d := v.(type) {
	case paragraphData:
		r.paragraph(d)
	case headerData:
		r.header(d)
	case listData:
		r.list(d)
	case quoteData:
		r.quote(d)
	case struct{}:
		r.w("<hr>\n")
	case imageData:
		r.image(d)
	case rawData:
		r.w(d.HTML, "\n") // Vertrauenssache: nur Admins speichern Raw-Blöcke
	case faqData:
		r.faqBlock(d)
	case answerData:
		r.answer(d)
	case ctaData:
		r.cta(d)
	}
}

func (r *renderer) paragraph(d paragraphData) {
	if t := SanitizeInline(d.Text); plainText(t) != "" || strings.Contains(t, "<br>") {
		r.w("<p>", t, "</p>\n")
	}
}

func (r *renderer) header(d headerData) {
	t := SanitizeInline(d.Text)
	h := "h" + strconv.Itoa(d.Level)
	r.w("<", h, ` id="`, r.ids.id(plainText(t)), `">`, t, "</", h, ">\n")
}

func (r *renderer) list(d listData) {
	tag := "ul"
	if d.Style == "ordered" {
		tag = "ol"
	}
	r.items(tag, d.Items)
	r.w("\n")
}

func (r *renderer) items(tag string, items []listItem) {
	if len(items) == 0 {
		return
	}
	r.w("<", tag, ">")
	for _, it := range items {
		r.w("<li>", SanitizeInline(it.Content))
		r.items(tag, it.Items)
		r.w("</li>")
	}
	r.w("</", tag, ">")
}

func (r *renderer) quote(d quoteData) {
	t := SanitizeInline(d.Text)
	if plainText(t) == "" {
		return
	}
	c := SanitizeInline(d.Caption)
	if plainText(c) == "" {
		r.w("<blockquote><p>", t, "</p></blockquote>\n")
		return
	}
	r.w("<figure><blockquote><p>", t, "</p></blockquote><figcaption>", c, "</figcaption></figure>\n")
}

func (r *renderer) image(d imageData) {
	src, ok := safeImageURL(d.url())
	if !ok {
		return // nur /… oder https:// – sonst Block auslassen
	}
	r.w(`<figure><img src="`, html.EscapeString(src), `"`)
	if set := srcset(d.Variants); set != "" {
		r.w(` srcset="`, html.EscapeString(set), `" sizes="(min-width: 48rem) 42rem, 100vw"`)
	}
	if d.Width > 0 && d.Height > 0 {
		r.w(` width="`, strconv.Itoa(d.Width), `" height="`, strconv.Itoa(d.Height), `"`)
	}
	r.w(` alt="`, html.EscapeString(strings.TrimSpace(d.Alt)), `" loading="lazy" decoding="async">`)
	if c := SanitizeInline(d.Caption); plainText(c) != "" {
		r.w("<figcaption>", c, "</figcaption>")
	}
	r.w("</figure>\n")
}

// srcset baut „/a-480.webp 480w, …“ aus geprüften Varianten (ownPath);
// ungültige werden übersprungen, eine einzelne Variante braucht kein srcset.
func srcset(vs []imageVariant) string {
	var parts []string
	for _, v := range vs {
		if ownPath(v.URL) && v.Width > 0 && v.Width <= maxImageSide {
			parts = append(parts, v.URL+" "+strconv.Itoa(v.Width)+"w")
		}
	}
	if len(parts) < 2 {
		return ""
	}
	return strings.Join(parts, ", ")
}

func (r *renderer) faqBlock(d faqData) {
	var items []FAQItem
	var b strings.Builder
	for _, it := range d.Items {
		q, a := SanitizeInline(it.Question), SanitizeInline(it.Answer)
		if plainText(q) == "" || plainText(a) == "" {
			continue
		}
		b.WriteString("<details><summary>" + q + "</summary><div>" + a + "</div></details>")
		items = append(items, FAQItem{Question: plainText(q), Answer: plainText(a)})
	}
	if len(items) == 0 {
		return
	}
	r.faq = append(r.faq, items...)
	r.w(`<section class="faq">`, b.String(), "</section>\n")
}

func (r *renderer) answer(d answerData) {
	q, a := SanitizeInline(d.Question), SanitizeInline(d.Answer)
	r.w(`<section class="answer"><h2 id="`, r.ids.id(plainText(q)), `">`, q, `</h2><p class="answer-short">`, a, "</p>")
	if det := SanitizeInline(d.Details); plainText(det) != "" {
		r.w(`<div class="answer-details"><p>`, det, "</p></div>")
	}
	r.w("</section>\n")
	r.faq = append(r.faq, FAQItem{Question: plainText(q), Answer: plainText(a)})
}

func (r *renderer) cta(d ctaData) {
	t := SanitizeInline(d.Text)
	href, external, ok := safeHref(d.URL)
	label := strings.TrimSpace(d.Label)
	if plainText(t) == "" && (!ok || label == "") {
		return
	}
	r.w(`<p class="cta">`, t)
	if ok && label != "" {
		if plainText(t) != "" {
			r.w(" ")
		}
		r.w(`<a class="cta-button" href="`, html.EscapeString(href), `"`)
		if external {
			r.w(` rel="noopener"`)
		}
		r.w(">", html.EscapeString(label), "</a>")
	}
	r.w("</p>\n")
}
