/* STYRIXA – eigene Editor.js-Blöcke (Vanilla-JS, kein Build-Schritt).
 *
 * Bild (URL-Eingabe, Upload folgt mit dem Media-Modul), FAQ, Answer-First,
 * Call-to-Action und eine Liste ohne Checkliste. Die gespeicherten Daten
 * entsprechen den Strukturen in blocks.go; der Server prüft sie (Validate)
 * und bereinigt Inline-HTML beim Ausliefern (SanitizeInline).
 */
(function () {
	"use strict";

	// Inline-HTML wie die Server-Allowlist (sanitize.go).
	var INLINE = { b: true, strong: true, i: true, em: true, u: true, mark: true, code: true, br: true, a: { href: true } };

	function el(tag, cls, text) {
		var e = document.createElement(tag);
		if (cls) {
			e.className = cls;
		}
		if (text) {
			e.textContent = text;
		}
		return e;
	}

	function clean(api, html) {
		var out = api.sanitizer.clean(html || "", INLINE).trim();
		return out.replace(/(<br\s*\/?>)+$/i, ""); // Editor.js hängt gern ein <br> an
	}

	// Mehrzeiliges Feld mit Inline-Formatierung (fett, Link …).
	function editable(api, html, label, readOnly) {
		var e = el("div", "styrixa-field cdx-input");
		e.contentEditable = readOnly ? "false" : "true";
		e.setAttribute("role", "textbox");
		e.setAttribute("aria-label", label);
		e.dataset.placeholder = label;
		e.innerHTML = clean(api, html);
		return e;
	}

	// Einzeiliges Feld ohne Formatierung (URL, Alt-Text, Button-Beschriftung).
	function input(value, label, readOnly) {
		var e = el("input", "styrixa-input cdx-input");
		e.type = "text";
		e.value = value || "";
		e.placeholder = label;
		e.setAttribute("aria-label", label);
		e.readOnly = !!readOnly;
		return e;
	}

	function button(text, onClick) {
		var b = el("button", "styrixa-btn", text);
		b.type = "button"; // nie ein Formular absenden
		b.addEventListener("click", onClick);
		return b;
	}

	function icon(path) {
		return '<svg xmlns="http://www.w3.org/2000/svg" width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">' + path + "</svg>";
	}

	// URL-Regeln wie render.go: Bilder nur relativ ab „/“ (ohne „//“) oder https://.
	function okImageURL(u) {
		return u === "" || (u.charAt(0) === "/" && u.charAt(1) !== "/") || /^https:\/\//i.test(u);
	}
	// Links wie safeHref: https, http, mailto, tel, „/“, „#“.
	function okHref(u) {
		return u === "" || /^(https?:\/\/|mailto:|tel:|#)/i.test(u) || (u.charAt(0) === "/" && u.charAt(1) !== "/");
	}

	function checked(field, hint, ok) {
		field.setAttribute("aria-invalid", ok ? "false" : "true");
		hint.hidden = ok;
	}

	/* ---------- Bild ---------- */

	function ImageBlock(opts) {
		var d = opts.data || {};
		this.api = opts.api;
		this.readOnly = opts.readOnly;
		this.data = { url: d.url || (d.file && d.file.url) || "", alt: d.alt || "", caption: d.caption || "" };
	}
	ImageBlock.toolbox = { title: "Bild", icon: icon('<rect x="3" y="3" width="18" height="18" rx="2"/><circle cx="9" cy="9" r="2"/><path d="m21 15-5-5L5 21"/>') };
	ImageBlock.enableLineBreaks = true;
	ImageBlock.isReadOnlySupported = true;
	ImageBlock.prototype.render = function () {
		var self = this;
		var w = el("div", "styrixa-block styrixa-image");
		w.appendChild(el("p", "styrixa-block__title", "Bild"));
		this.url = input(this.data.url, "Bild-Adresse, z. B. /media/foto.webp oder https://…", this.readOnly);
		this.url.inputMode = "url";
		var hint = el("p", "styrixa-warn", "Nur Adressen ab „/“ (z. B. /media/…) oder mit https:// – sonst wird das Bild nicht angezeigt.");
		this.alt = input(this.data.alt, "Alternativtext (beschreibt das Bild für Screenreader und Suchmaschinen)", this.readOnly);
		this.caption = editable(this.api, this.data.caption, "Bildunterschrift (optional)", this.readOnly);
		var preview = el("img", "styrixa-image__preview");
		preview.alt = "";
		var update = function () {
			var u = self.url.value.trim();
			checked(self.url, hint, okImageURL(u));
			// Vorschau nur für eigene Dateien: Admin-CSP erlaubt Bilder nur von 'self'.
			if (u && u.charAt(0) === "/" && u.charAt(1) !== "/") {
				preview.src = u;
				preview.hidden = false;
			} else {
				preview.removeAttribute("src");
				preview.hidden = true;
			}
		};
		this.url.addEventListener("input", update);
		update();
		w.append(this.url, hint, preview, this.alt, this.caption);
		return w;
	};
	ImageBlock.prototype.save = function () {
		return { url: this.url.value.trim(), alt: this.alt.value.trim(), caption: clean(this.api, this.caption.innerHTML) };
	};

	/* ---------- FAQ ---------- */

	function FAQBlock(opts) {
		this.api = opts.api;
		this.readOnly = opts.readOnly;
		this.items = ((opts.data && opts.data.items) || []).slice();
		if (this.items.length === 0) {
			this.items.push({ question: "", answer: "" });
		}
		this.rows = [];
	}
	FAQBlock.toolbox = { title: "FAQ", icon: icon('<circle cx="12" cy="12" r="10"/><path d="M9.1 9a3 3 0 0 1 5.8 1c0 2-3 3-3 3"/><path d="M12 17h.01"/>') };
	FAQBlock.enableLineBreaks = true;
	FAQBlock.isReadOnlySupported = true;
	FAQBlock.prototype.addRow = function (item) {
		var self = this;
		var row = el("div", "styrixa-faq__item");
		var q = editable(this.api, item.question, "Frage", this.readOnly);
		var a = editable(this.api, item.answer, "Antwort", this.readOnly);
		q.classList.add("styrixa-field--strong");
		row.append(q, a);
		var entry = { row: row, q: q, a: a };
		if (!this.readOnly) {
			row.appendChild(button("Frage entfernen", function () {
				self.rows.splice(self.rows.indexOf(entry), 1);
				row.remove(); // Editor.js meldet die DOM-Änderung als onChange
			}));
		}
		this.rows.push(entry);
		this.list.appendChild(row);
		return entry;
	};
	FAQBlock.prototype.render = function () {
		var self = this;
		var w = el("div", "styrixa-block styrixa-faq");
		w.appendChild(el("p", "styrixa-block__title", "Häufige Fragen"));
		this.list = el("div", "styrixa-faq__list");
		w.appendChild(this.list);
		this.items.forEach(function (it) {
			self.addRow(it);
		});
		if (!this.readOnly) {
			w.appendChild(button("Frage hinzufügen", function () {
				self.addRow({ question: "", answer: "" }).q.focus();
			}));
		}
		return w;
	};
	FAQBlock.prototype.save = function () {
		var api = this.api;
		return {
			items: this.rows.map(function (r) {
				return { question: clean(api, r.q.innerHTML), answer: clean(api, r.a.innerHTML) };
			}).filter(function (it) {
				return it.question !== "" || it.answer !== "";
			})
		};
	};

	/* ---------- Answer-First ---------- */

	function AnswerBlock(opts) {
		var d = opts.data || {};
		this.api = opts.api;
		this.readOnly = opts.readOnly;
		this.data = { question: d.question || "", answer: d.answer || "", details: d.details || "" };
	}
	AnswerBlock.toolbox = { title: "Antwort zuerst", icon: icon('<path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/><path d="M8 9h8M8 13h5"/>') };
	AnswerBlock.enableLineBreaks = true;
	AnswerBlock.isReadOnlySupported = true;
	AnswerBlock.prototype.render = function () {
		var w = el("div", "styrixa-block styrixa-answer");
		w.appendChild(el("p", "styrixa-block__title", "Antwort zuerst"));
		this.q = editable(this.api, this.data.question, "Frage (wird als Überschrift ausgegeben)", this.readOnly);
		this.q.classList.add("styrixa-field--strong");
		this.a = editable(this.api, this.data.answer, "Kurzantwort in ein bis zwei Sätzen", this.readOnly);
		this.det = editable(this.api, this.data.details, "Details (optional)", this.readOnly);
		w.append(this.q, this.a, this.det);
		return w;
	};
	AnswerBlock.prototype.save = function () {
		return { question: clean(this.api, this.q.innerHTML), answer: clean(this.api, this.a.innerHTML), details: clean(this.api, this.det.innerHTML) };
	};

	/* ---------- Call-to-Action ---------- */

	function CTABlock(opts) {
		var d = opts.data || {};
		this.api = opts.api;
		this.readOnly = opts.readOnly;
		this.data = { text: d.text || "", label: d.label || "", url: d.url || "" };
	}
	CTABlock.toolbox = { title: "Call-to-Action", icon: icon('<rect x="3" y="8" width="18" height="8" rx="4"/><path d="M10 12h4"/>') };
	CTABlock.enableLineBreaks = true;
	CTABlock.isReadOnlySupported = true;
	CTABlock.prototype.render = function () {
		var self = this;
		var w = el("div", "styrixa-block styrixa-cta");
		w.appendChild(el("p", "styrixa-block__title", "Call-to-Action"));
		this.text = editable(this.api, this.data.text, "Text (optional)", this.readOnly);
		this.label = input(this.data.label, "Beschriftung des Buttons, z. B. „Jetzt anfragen“", this.readOnly);
		this.url = input(this.data.url, "Ziel-Adresse: /kontakt, https://…, mailto: oder tel:", this.readOnly);
		this.url.inputMode = "url";
		var hint = el("p", "styrixa-warn", "Erlaubt sind https://, http://, mailto:, tel:, Adressen ab „/“ und „#“.");
		var update = function () {
			checked(self.url, hint, okHref(self.url.value.trim()));
		};
		this.url.addEventListener("input", update);
		update();
		w.append(this.text, this.label, this.url, hint);
		return w;
	};
	CTABlock.prototype.save = function () {
		return { text: clean(this.api, this.text.innerHTML), label: this.label.value.trim(), url: this.url.value.trim() };
	};

	/* ---------- Liste ohne Checkliste ---------- */
	// Der Renderer kennt nur geordnete und ungeordnete Listen; das offizielle
	// Plugin bietet die Checkliste fest an – hier ausgeblendet.

	var List = null;
	if (typeof window.EditorjsList === "function") {
		List = class extends window.EditorjsList {
			static get toolbox() {
				return super.toolbox.filter(function (t) {
					return !t.data || t.data.style !== "checklist";
				});
			}
			renderSettings() {
				var checklist = this.api.i18n.t("Checklist");
				return super.renderSettings().filter(function (item) {
					return item.label !== checklist;
				});
			}
		};
	}

	window.StyrixaBlocks = { Image: ImageBlock, FAQ: FAQBlock, Answer: AnswerBlock, CTA: CTABlock, List: List, okImageURL: okImageURL, okHref: okHref };
})();
