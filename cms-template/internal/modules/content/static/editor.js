/* STYRIXA – Block-Editor der Seiten (Session 3.2, ADR-003).
 *
 * Startet Editor.js mit den vendorten Plugins und den eigenen Blöcken,
 * speichert per fetch als Entwurf (POST …/draft, JSON) und zeigt die
 * Rückmeldung des Servers an. Validierung und Bereinigung macht der Server.
 */
(function () {
	"use strict";

	var root = document.getElementById("editor");
	if (!root || typeof window.EditorJS !== "function" || !window.StyrixaBlocks) {
		return;
	}

	var B = window.StyrixaBlocks;
	var titleField = document.getElementById("page-title");
	var saveButton = document.getElementById("editor-save");
	var statusEl = document.getElementById("editor-status");
	var dirty = false;
	var saving = null; // laufendes Speichern (Promise)

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

	function status(text, isError) {
		statusEl.textContent = text;
		statusEl.classList.toggle("err", !!isError);
	}

	// Änderungen erkennt der Vergleich mit dem zuletzt gespeicherten Stand –
	// nicht einzelne Events (Editor.js meldet onChange verzögert, <input>-Felder
	// eigener Blöcke gar nicht).
	var saved = null; // {blocks, title} als JSON des letzten gespeicherten Stands

	function snapshot(out) {
		return JSON.stringify({ blocks: out.blocks, title: titleField.value });
	}

	function setDirty(v) {
		if (v && !dirty) {
			status("Ungespeicherte Änderungen");
		}
		dirty = v;
	}

	// changed liefert, ob der aktuelle Stand vom gespeicherten abweicht.
	function changed() {
		return editor.save().then(function (out) {
			return saved === null || snapshot(out) !== saved;
		});
	}

	function refreshDirty() {
		if (saved !== null) {
			changed().then(setDirty);
		}
	}

	var editor = new window.EditorJS({
		holder: "editorjs",
		data: data,
		tools: tools,
		inlineToolbar: inline,
		i18n: i18n,
		style: { nonce: window.StyrixaStyleNonce }, // siehe editor-nonce.js
		minHeight: 120,
		onChange: refreshDirty,
		onReady: function () {
			editor.save().then(function (out) {
				saved = snapshot(out);
			});
		}
	});
	root.addEventListener("input", refreshDirty);
	titleField.addEventListener("input", refreshDirty);

	function time() {
		var d = new Date();
		return String(d.getHours()).padStart(2, "0") + ":" + String(d.getMinutes()).padStart(2, "0");
	}

	// save speichert den aktuellen Stand als neue Revision. Ergebnis: true = gespeichert.
	function save() {
		if (saving) {
			return saving;
		}
		saveButton.disabled = true;
		status("Wird gespeichert …");
		var sent;
		saving = editor.save().then(function (out) {
			sent = snapshot(out);
			return fetch(root.dataset.draftUrl, {
				method: "POST",
				credentials: "same-origin",
				headers: { "Content-Type": "application/json", "X-CSRF-Token": root.dataset.csrf },
				body: JSON.stringify({ title: titleField.value, format: "blocks", body: out })
			});
		}).then(function (res) {
			return res.json().catch(function () {
				return {};
			}).then(function (j) {
				if (!res.ok) {
					throw new Error(j.error || "Speichern fehlgeschlagen (" + res.status + ").");
				}
				saved = sent;
				dirty = false;
				status("Entwurf gespeichert um " + time() + ".");
				refreshDirty(); // Änderungen während des Speicherns
				return true;
			});
		}).catch(function (err) {
			status(err && err.message ? err.message : "Speichern fehlgeschlagen.", true);
			return false;
		}).finally(function () {
			saving = null;
			saveButton.disabled = false;
		});
		return saving;
	}

	saveButton.addEventListener("click", function () {
		save();
	});
	document.addEventListener("keydown", function (e) {
		if ((e.ctrlKey || e.metaKey) && !e.altKey && e.key.toLowerCase() === "s") {
			e.preventDefault();
			save();
		}
	});

	// Veröffentlichen: ungespeicherte Änderungen zuerst als Entwurf sichern,
	// sonst ginge ein älterer Stand online.
	document.querySelectorAll("form[data-save-first]").forEach(function (form) {
		form.addEventListener("submit", function (e) {
			e.preventDefault();
			(saving || Promise.resolve()).then(changed).then(function (c) {
				return c ? save() : true;
			}).then(function (ok) {
				if (ok) {
					form.submit(); // löst kein weiteres submit-Event aus
				}
			});
		});
	});
})();
