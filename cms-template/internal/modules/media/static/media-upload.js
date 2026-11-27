/* STYRIXA – Bild-Upload mit Verkleinerung im Browser (ADR-001, Session 4.1).
 *
 * Erzeugt aus einer Datei bis zu drei Varianten (480, 960, 1920 px, nur
 * Breiten ≤ Original; schmälere Originale als genau eine Variante), als WebP
 * (0,82) oder – wenn der Browser kein WebP aus dem Canvas erzeugt (Safari
 * liefert dann PNG) – als JPEG (0,85). Der Server prüft alles erneut.
 *
 * window.StyrixaMediaUpload.upload(file, {alt, decorative, folderId, csrf, onStatus})
 *   → Promise<Bild>  (Bild = {id, name, url, thumb, alt, width, height, variants})
 */
(function () {
	"use strict";

	var WIDTHS = [480, 960, 1920];

	function csrfToken() {
		try {
			return JSON.parse(document.body.getAttribute("hx-headers") || "{}")["X-CSRF-Token"] || "";
		} catch (e) {
			return "";
		}
	}

	function targetWidths(w) {
		if (w < WIDTHS[0]) {
			return [w];
		}
		return WIDTHS.filter(function (x) { return x <= w; });
	}

	function toBlob(canvas, type, quality) {
		return new Promise(function (resolve, reject) {
			canvas.toBlob(function (b) {
				if (b) {
					resolve(b);
				} else {
					reject(new Error("Das Bild konnte im Browser nicht umgewandelt werden."));
				}
			}, type, quality);
		});
	}

	function draw(bitmap, w, h, opaque) {
		var c = document.createElement("canvas");
		c.width = w;
		c.height = h;
		var ctx = c.getContext("2d");
		if (opaque) { // JPEG kennt keine Transparenz: weiß statt schwarz
			ctx.fillStyle = "#ffffff";
			ctx.fillRect(0, 0, w, h);
		}
		ctx.imageSmoothingEnabled = true;
		ctx.imageSmoothingQuality = "high";
		ctx.drawImage(bitmap, 0, 0, w, h);
		return c;
	}

	// variants liefert [{blob, width}] in einem einheitlichen Format.
	function variants(bitmap) {
		var widths = targetWidths(bitmap.width);
		var sizes = widths.map(function (w) {
			return { width: w, height: Math.max(1, Math.round(bitmap.height * w / bitmap.width)) };
		});
		function encode(type, quality, opaque) {
			return Promise.all(sizes.map(function (s) {
				return toBlob(draw(bitmap, s.width, s.height, opaque), type, quality).then(function (b) {
					return { blob: b, width: s.width };
				});
			}));
		}
		return encode("image/webp", 0.82, false).then(function (out) {
			if (out.every(function (v) { return v.blob.type === "image/webp"; })) {
				return out;
			}
			return encode("image/jpeg", 0.85, true); // Browser ohne Canvas-WebP
		});
	}

	function baseName(name) {
		return String(name || "").replace(/\.[A-Za-z0-9]{1,5}$/, "").slice(0, 120);
	}

	function upload(file, opts) {
		opts = opts || {};
		var status = opts.onStatus || function () {};
		if (!file) {
			return Promise.reject(new Error("Bitte eine Bilddatei wählen."));
		}
		if (!/^image\/(jpeg|png|webp)$/.test(file.type)) {
			return Promise.reject(new Error("Nur JPEG-, PNG- und WebP-Bilder können hochgeladen werden."));
		}
		if (!opts.decorative && !String(opts.alt || "").trim()) {
			return Promise.reject(new Error("Bitte einen Alternativtext eingeben – oder „Dekoratives Bild“ anhaken."));
		}
		if (typeof createImageBitmap !== "function") {
			return Promise.reject(new Error("Dieser Browser kann Bilder nicht verkleinern. Bitte einen aktuellen Browser verwenden."));
		}
		status("Bild wird verkleinert …");
		return createImageBitmap(file, { imageOrientation: "from-image" }).then(function (bitmap) {
			return variants(bitmap).then(function (vs) {
				if (bitmap.close) {
					bitmap.close();
				}
				var fd = new FormData();
				fd.append("name", baseName(file.name));
				fd.append("alt", opts.decorative ? "" : String(opts.alt || "").trim());
				fd.append("decorative", opts.decorative ? "1" : "0");
				fd.append("folder_id", String(opts.folderId || 0));
				vs.forEach(function (v) {
					fd.append("variant", v.blob, v.width + (v.blob.type === "image/webp" ? ".webp" : ".jpg"));
				});
				status("Wird hochgeladen …");
				return fetch("/admin/media/upload", {
					method: "POST",
					body: fd,
					credentials: "same-origin",
					headers: { "X-CSRF-Token": opts.csrf || csrfToken() }
				});
			});
		}, function () {
			throw new Error("Die Datei ist kein lesbares Bild.");
		}).then(function (res) {
			return res.json().catch(function () { return {}; }).then(function (body) {
				if (!res.ok) {
					throw new Error(body.error || "Das Hochladen ist fehlgeschlagen (" + res.status + ").");
				}
				status("Hochgeladen.");
				return body;
			});
		});
	}

	window.StyrixaMediaUpload = { upload: upload, targetWidths: targetWidths, csrfToken: csrfToken };
})();
