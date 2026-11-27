/* STYRIXA – Bildwähler für den Editor (Erweiterungspunkt core.ExtEditor, Session 4.1).
 *
 * window.StyrixaMediaPicker.pick() → Promise<Bild | null>
 *   Bild = {url, alt, width, height, variants: [{url, width}]}
 * Öffnet einen <dialog> mit Ordnern, Suche, Bildern und Upload. Alle Texte
 * aus der Datenbank werden per textContent eingesetzt, nie als HTML.
 */
(function () {
	"use strict";

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

	function button(text, cls) {
		var b = el("button", cls, text);
		b.type = "button";
		return b;
	}

	function pick() {
		return new Promise(function (resolve) {
			var state = { folder: 0, q: "", offset: 0 };
			var done = false;

			var dlg = el("dialog", "media-picker");
			dlg.setAttribute("aria-labelledby", "media-picker-h");
			var head = el("div", "media-picker__head");
			var h = el("h2", "", "Bild aus der Mediathek");
			h.id = "media-picker-h";
			var close = button("Schließen", "link");
			head.append(h, close);

			var trail = el("nav", "media-trail");
			trail.setAttribute("aria-label", "Ordnerpfad");
			var search = el("input", "media-picker__search");
			search.type = "search";
			search.placeholder = "Suchen nach Name oder Alternativtext";
			search.setAttribute("aria-label", "Bilder suchen");
			var folders = el("ul", "media-folders");
			var grid = el("ul", "media-grid media-grid--pick");
			var more = button("Weitere Bilder", "link");
			more.hidden = true;
			var msg = el("p", "hint");
			msg.setAttribute("role", "status");

			// Upload direkt im Dialog
			var up = el("details", "media-picker__upload");
			up.appendChild(el("summary", "", "Neues Bild hochladen"));
			var fileIn = el("input");
			fileIn.type = "file";
			fileIn.accept = "image/jpeg,image/png,image/webp";
			fileIn.setAttribute("aria-label", "Bilddatei");
			var alt = el("input");
			alt.maxLength = 250;
			alt.placeholder = "Alternativtext – was zeigt das Bild?";
			alt.setAttribute("aria-label", "Alternativtext");
			var decoLabel = el("label", "check");
			var deco = el("input");
			deco.type = "checkbox";
			decoLabel.append(deco, document.createTextNode(" Dekoratives Bild (kein Alternativtext)"));
			var upBtn = button("Hochladen und einfügen");
			var upMsg = el("p", "hint");
			upMsg.setAttribute("role", "status");
			up.append(fileIn, alt, decoLabel, upBtn, upMsg);

			dlg.append(head, up, search, trail, folders, grid, more, msg);
			document.body.appendChild(dlg);

			function finish(result) {
				if (done) {
					return;
				}
				done = true;
				dlg.close();
				dlg.remove();
				resolve(result);
			}

			function chosen(p) {
				return { url: p.url, alt: p.alt || "", width: p.width, height: p.height, variants: p.variants || [] };
			}

			function load(append) {
				var qs = new URLSearchParams({ folder: String(state.folder), q: state.q, offset: String(state.offset) });
				msg.textContent = "Lädt …";
				fetch("/admin/media/api?" + qs.toString(), { credentials: "same-origin" })
					.then(function (r) {
						return r.json().then(function (b) {
							if (!r.ok) {
								throw new Error(b.error || "Die Mediathek konnte nicht geladen werden.");
							}
							return b;
						});
					})
					.then(function (b) { render(b, append); }, function (e) { msg.textContent = e.message; });
			}

			function go(folder) {
				state.folder = folder;
				state.offset = 0;
				state.q = "";
				search.value = "";
				load(false);
			}

			function render(b, append) {
				trail.replaceChildren();
				var root = button("Alle Medien", "link");
				root.addEventListener("click", function () { go(0); });
				trail.appendChild(root);
				(b.trail || []).forEach(function (f) {
					trail.appendChild(document.createTextNode(" / "));
					var a = button(f.name, "link");
					a.addEventListener("click", function () { go(f.id); });
					trail.appendChild(a);
				});
				folders.replaceChildren();
				(b.folders || []).forEach(function (f) {
					var li = el("li");
					var a = button("📁 " + f.name, "link");
					a.addEventListener("click", function () { go(f.id); });
					li.appendChild(a);
					folders.appendChild(li);
				});
				if (!append) {
					grid.replaceChildren();
				}
				(b.files || []).forEach(function (p) {
					var li = el("li");
					var btn = button("", "media-pick");
					var img = el("img");
					img.src = p.thumb;
					img.alt = "";
					img.width = p.width;
					img.height = p.height;
					img.loading = "lazy";
					btn.append(img, el("span", "media-name", p.name));
					btn.setAttribute("aria-label", p.name + (p.alt ? " – " + p.alt : " – dekorativ"));
					btn.addEventListener("click", function () { finish(chosen(p)); });
					li.appendChild(btn);
					grid.appendChild(li);
				});
				more.hidden = !b.more;
				msg.textContent = grid.children.length ? "" : (state.q ? "Nichts gefunden." : "Noch keine Bilder in diesem Ordner.");
			}

			var timer;
			search.addEventListener("input", function () {
				clearTimeout(timer);
				timer = setTimeout(function () {
					state.q = search.value.trim();
					state.offset = 0;
					load(false);
				}, 250);
			});
			more.addEventListener("click", function () {
				state.offset = grid.children.length;
				load(true);
			});
			deco.addEventListener("change", function () { alt.disabled = deco.checked; });
			upBtn.addEventListener("click", function () {
				if (!window.StyrixaMediaUpload) {
					return;
				}
				upBtn.disabled = true;
				upMsg.className = "hint";
				window.StyrixaMediaUpload.upload(fileIn.files[0], {
					alt: alt.value,
					decorative: deco.checked,
					folderId: state.folder,
					onStatus: function (t) { upMsg.textContent = t; }
				}).then(function (p) {
					finish(chosen(p));
				}, function (e) {
					upMsg.textContent = e.message;
					upMsg.className = "err";
					upBtn.disabled = false;
				});
			});
			close.addEventListener("click", function () { finish(null); });
			dlg.addEventListener("cancel", function (ev) {
				ev.preventDefault();
				finish(null);
			});

			dlg.showModal();
			load(false);
		});
	}

	window.StyrixaMediaPicker = { pick: pick };
})();
