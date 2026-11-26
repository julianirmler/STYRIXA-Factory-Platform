/* STYRIXA – CSP-Brücke für vendorte Editor.js-Dateien.
 *
 * Editor.js und seine Plugins fügen ihr CSS zur Laufzeit als <style> in
 * <head> ein (teils erst später, z. B. Tooltips). Die Admin-CSP erlaubt
 * <style> nur mit der Nonce dieser Anfrage. Dieses Skript läuft vor allen
 * anderen und gibt <style>-Elementen, die in <head> landen, die Nonce mit.
 * Skripte bleiben auf 'self' beschränkt – die Nonce gilt nur für Styles.
 */
(function () {
	"use strict";
	var nonce = document.currentScript && document.currentScript.nonce;
	if (!nonce) {
		return;
	}
	var head = document.head;
	function withNonce(name) {
		var original = head[name];
		head[name] = function () {
			for (var i = 0; i < arguments.length; i++) {
				var el = arguments[i];
				if (el instanceof HTMLStyleElement && !el.nonce) {
					el.nonce = nonce;
				}
			}
			return original.apply(this, arguments);
		};
	}
	withNonce("appendChild");
	withNonce("insertBefore");
	withNonce("prepend");
	withNonce("append");
	window.StyrixaStyleNonce = nonce;
})();
