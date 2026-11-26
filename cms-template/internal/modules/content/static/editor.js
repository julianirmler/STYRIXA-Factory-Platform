/* STYRIXA – Editor der Seiten (Sessions 3.2/3.3, ADR-003).
 *
 * Block-Modus: Editor.js mit den vendorten Plugins und den eigenen Blöcken.
 * HTML-Modus (nur Admin): <textarea>. Beide speichern per fetch als Entwurf
 * (POST …/draft, JSON) – auf Knopfdruck, mit Strg/⌘+S und alle 5 s
 * automatisch, wenn sich etwas geändert hat. Validierung, Rollenprüfung und
 * Bereinigung macht der Server.
 */
(function () {
	"use strict";

	var root = document.getElementById("editor");
	var mode = root ? root.dataset.mode : "";
	var adapter = null;
	if (mode === "blocks" && typeof window.EditorJS === "function" && window.StyrixaBlocks) {
		adapter = blocksAdapter();
	} else if (mode === "html") {
		adapter = htmlAdapter();
	}

	var AUTOSAVE_MS = 5000;
	var KEEPALIVE_MAX = 60000; // fetch keepalive erlaubt höchstens 64 KiB

	var titleField = document.getElementById("page-title");
	var saveButton = document.getElementById("editor-save");
	var statusEl = document.getElementById("editor-status");
	var noticeEl = document.getElementById("editor-notice");
	var previewLink = document.getElementById("editor-preview");
	var rev = root ? Number(root.dataset.rev) || 0 : 0; // Basis für die Konfliktprüfung
	var saved = null;  // Stand des letzten erfolgreichen Speicherns (JSON), null = Editor nicht bereit
	var failed = null; // Stand, dessen Speichern der Server abgelehnt hat – Auto-Save wiederholt ihn nicht
	var dirty = false;
	var saving = null; // laufendes Speichern (Promise)

	function status(text, isError) {
		statusEl.textContent = text;
		statusEl.classList.toggle("err", !!isError);
	}

	function time() {
		var d = new Date();
		return String(d.getHours()).padStart(2, "0") + ":" + String(d.getMinutes()).padStart(2, "0");
	}

	// read liefert den aktuellen Stand: {format, body, key, snap}. snap vergleicht
	// Stände (Änderungserkennung über Vergleich, nicht über einzelne Events:
	// Editor.js meldet onChange verzögert, <input>-Felder eigener Blöcke gar nicht).
	function read() {
		return adapter.read().then(function (s) {
			s.snap = JSON.stringify({ body: s.key, title: titleField.value });
			return s;
		});
	}

	function setDirty(v) {
		if (v && !dirty && !saving) {
			status("Ungespeicherte Änderungen");
		}
		dirty = v;
	}

	// changed liefert, ob der aktuelle Stand vom gespeicherten abweicht.
	function changed() {
		return read().then(function (s) {
			return saved === null || s.snap !== saved;
		});
	}

	function refreshDirty() {
		if (saved !== null) {
			changed().then(setDirty);
		}
	}

	// save speichert den aktuellen Stand. auto = automatisch (der Server fasst
	// Auto-Saves zusammen). Ergebnis: true = gespeichert.
	function save(auto, keepalive) {
		if (saving) {
			return saving;
		}
		saveButton.disabled = true;
		if (!auto) {
			status("Wird gespeichert …");
		}
		var sent;
		saving = read().then(function (s) {
			sent = s.snap;
			var payload = JSON.stringify({ title: titleField.value, format: s.format, body: s.body, base_revision_id: rev, autosave: !!auto });
			return fetch(root.dataset.draftUrl, {
				method: "POST",
				credentials: "same-origin",
				keepalive: !!keepalive && payload.length < KEEPALIVE_MAX,
				headers: { "Content-Type": "application/json", "X-CSRF-Token": root.dataset.csrf },
				body: payload
			});
		}).then(function (res) {
			return res.json().catch(function () {
				return {};
			}).then(function (j) {
				if (!res.ok) {
					throw new Error(j.error || "Speichern fehlgeschlagen (" + res.status + ").");
				}
				rev = j.revision_id || rev;
				saved = sent;
				failed = null;
				dirty = false;
				status((auto ? "Automatisch gespeichert um " : "Entwurf gespeichert um ") + time() + ".");
				if (j.notice) {
					noticeEl.textContent = j.notice;
					noticeEl.hidden = false;
				}
				return true;
			});
		}).catch(function (err) {
			failed = sent;
			status(err && err.message ? err.message : "Speichern fehlgeschlagen.", true);
			return false;
		}).finally(function () {
			saving = null;
			saveButton.disabled = false;
			refreshDirty(); // Änderungen während des Speicherns
		});
		return saving;
	}

	// flush sichert ungespeicherte Änderungen (vor Veröffentlichen, Vorschau, …).
	function flush(auto) {
		if (!adapter) {
			return Promise.resolve(true);
		}
		return (saving || Promise.resolve()).then(changed).then(function (c) {
			return c ? save(auto) : true;
		});
	}

	// autosave: nur bei Änderung, nie parallel, nie denselben abgelehnten Stand erneut.
	function autosave(keepalive) {
		if (saving || saved === null) {
			return;
		}
		read().then(function (s) {
			if (s.snap !== saved && s.snap !== failed) {
				save(true, keepalive);
			}
		});
	}

	function ready(initial) {
		saved = initial;
		window.setInterval(function () {
			autosave(false);
		}, AUTOSAVE_MS);
	}

	if (adapter) {
		adapter.start(refreshDirty).then(read).then(function (s) {
			ready(s.snap);
		});
		titleField.addEventListener("input", refreshDirty);
		saveButton.addEventListener("click", function () {
			save(false);
		});
		document.addEventListener("keydown", function (e) {
			if ((e.ctrlKey || e.metaKey) && !e.altKey && e.key.toLowerCase() === "s") {
				e.preventDefault();
				save(false);
			}
		});
		// Tab verlassen, App wechseln, Fenster schließen: sofort sichern (keepalive
		// überlebt das Schließen); zusätzlich warnt beforeunload.
		document.addEventListener("visibilitychange", function () {
			if (document.visibilityState === "hidden") {
				autosave(true);
			}
		});
		window.addEventListener("beforeunload", function (e) {
			if (dirty || saving) {
				e.preventDefault();
				e.returnValue = "";
			}
		});
		// Vorschau zeigt den gespeicherten Entwurf – vorher sichern. Das Fenster
		// wird sofort geöffnet (sonst blockiert der Browser das Pop-up).
		previewLink.addEventListener("click", function (e) {
			if (!dirty && !saving) {
				return;
			}
			e.preventDefault();
			var w = window.open("", "_blank");
			if (w) {
				w.opener = null;
			}
			flush(true).then(function (ok) {
				if (w) {
					w.location.href = previewLink.href;
				} else if (ok) {
					window.location.href = previewLink.href; // Pop-up blockiert: gespeichert, also im selben Tab
				}
			});
		});
	}

	// Formulare: data-confirm fragt nach, data-save-first sichert vorher den
	// Entwurf (sonst ginge z. B. beim Veröffentlichen ein älterer Stand online).
	document.querySelectorAll("form[data-save-first], form[data-confirm]").forEach(function (form) {
		form.addEventListener("submit", function (e) {
			e.preventDefault();
			var question = form.dataset.confirm;
			if (question && !window.confirm(question)) {
				return;
			}
			var first = form.hasAttribute("data-save-first") ? flush(false) : Promise.resolve(true);
			first.then(function (ok) {
				if (ok) {
					dirty = false; // gespeichert – keine beforeunload-Warnung beim Absenden
					form.submit(); // löst kein weiteres submit-Event aus
				}
			});
		});
	});

	function htmlAdapter() {
		var area = document.getElementById("html-body");
		return {
			start: function (onChange) {
				area.addEventListener("input", function () {
					setDirty(true); // sofort, damit beforeunload greift
					onChange();
				});
				return Promise.resolve();
			},
			read: function () {
				return Promise.resolve({ format: "html", body: area.value, key: area.value });
			}
		};
	}

	function blocksAdapter() {
		var B = window.StyrixaBlocks;
		var editor;
		var data;
		try {
			data = JSON.parse(root.dataset.body || "{}");
		} catch (e) {
			data = { blocks: [] };
		}

		// Inline-Werkzeuge = Allowlist des Servers (fett, kursiv, Link, Markierung, Code).
		var inline = ["bold", "italic", "link", "marker", "inlineCode"];

		var tools = {
			paragraph: { inlineToolbar: inline, config: { placeholder: "Text eingeben – mit „+“ weitere Blöcke einfügen" } },
			header: { class: window.Header, inlineToolbar: inline, config: { levels: [2, 3, 4], defaultLevel: 2, placeholder: "Überschrift" } },
			list: { class: B.List || window.EditorjsList, inlineToolbar: inline, config: { defaultStyle: "unordered", maxLevel: 3, counterTypes: ["numeric"] } },
			quote: { class: window.Quote, inlineToolbar: inline, config: { quotePlaceholder: "Zitat", captionPlaceholder: "Quelle (optional)" } },
			delimiter: window.Delimiter,
			image: { class: B.Image, inlineToolbar: inline },
			faq: { class: B.FAQ, inlineToolbar: inline },
			answer: { class: B.Answer, inlineToolbar: inline },
			cta: { class: B.CTA, inlineToolbar: inline },
			marker: { class: window.Marker },
			inlineCode: { class: window.InlineCode }
		};
		// Raw-HTML nur für Admins (der Server prüft die Rolle beim Speichern erneut).
		if (root.dataset.allowRaw === "1" && window.RawTool) {
			tools.raw = { class: window.RawTool, config: { placeholder: "HTML-Code – wird ungefiltert ausgegeben" } };
		}

		var i18n = {
			messages: {
				ui: {
					blockTunes: { toggler: { "Click to tune": "Klicken für Optionen", "or drag to move": "oder ziehen zum Verschieben" } },
					inlineToolbar: { converter: { "Convert to": "Umwandeln in" } },
					toolbar: { toolbox: { Add: "Hinzufügen" } },
					popover: { Filter: "Suchen", "Nothing found": "Nichts gefunden", "Convert to": "Umwandeln in" }
				},
				toolNames: {
					Text: "Absatz", Heading: "Überschrift", List: "Liste", "Unordered List": "Aufzählung", "Ordered List": "Nummerierte Liste",
					Quote: "Zitat", Delimiter: "Trennlinie", "Raw HTML": "HTML", Bold: "Fett", Italic: "Kursiv", Link: "Link",
					Marker: "Markierung", InlineCode: "Code"
				},
				tools: {
					link: { "Add a link": "Link-Adresse eingeben" },
					list: { Unordered: "Aufzählung", Ordered: "Nummeriert", Checklist: "Checkliste", "Start with": "Beginnt mit", "Counter type": "Zählweise", Numeric: "Zahlen" },
					header: { "Heading 2": "Überschrift 2", "Heading 3": "Überschrift 3", "Heading 4": "Überschrift 4" },
					quote: { "Align Left": "Linksbündig", "Align Center": "Zentriert" },
					stub: { "The block can not be displayed correctly.": "Der Block kann nicht angezeigt werden." }
				},
				blockTunes: {
					delete: { Delete: "Löschen", "Click to delete": "Zum Löschen erneut klicken" },
					moveUp: { "Move up": "Nach oben" },
					moveDown: { "Move down": "Nach unten" }
				}
			}
		};

		return {
			start: function (onChange) {
				return new Promise(function (resolve) {
					editor = new window.EditorJS({
						holder: "editorjs",
						data: data,
						tools: tools,
						inlineToolbar: inline,
						i18n: i18n,
						style: { nonce: window.StyrixaStyleNonce }, // siehe editor-nonce.js
						minHeight: 120,
						onChange: onChange,
						onReady: resolve
					});
					root.addEventListener("input", onChange);
				});
			},
			read: function () {
				return editor.save().then(function (out) {
					return { format: "blocks", body: out, key: out.blocks }; // ohne „time“ – ändert sich bei jedem save()
				});
			}
		};
	}
})();
