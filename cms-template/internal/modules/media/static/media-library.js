/* STYRIXA – Mediathek-Seite (Session 4.1): Upload-Formular und Rückfragen. */
(function () {
	"use strict";

	document.querySelectorAll("form[data-confirm]").forEach(function (form) {
		form.addEventListener("submit", function (ev) {
			if (!window.confirm(form.dataset.confirm)) {
				ev.preventDefault();
			}
		});
	});

	var box = document.querySelector("[data-media-upload]");
	if (!box || !window.StyrixaMediaUpload) {
		return;
	}
	var fileIn = box.querySelector("#media-file");
	var alt = box.querySelector("#media-alt");
	var deco = box.querySelector("#media-decorative");
	var start = box.querySelector("[data-media-upload-start]");
	var status = box.querySelector("[data-media-upload-status]");

	function setStatus(text, error) {
		status.textContent = text;
		status.className = error ? "err" : "hint";
	}

	deco.addEventListener("change", function () {
		alt.disabled = deco.checked;
	});

	start.addEventListener("click", function () {
		start.disabled = true;
		window.StyrixaMediaUpload.upload(fileIn.files[0], {
			alt: alt.value,
			decorative: deco.checked,
			folderId: Number(box.dataset.folder) || 0,
			csrf: box.dataset.csrf,
			onStatus: function (t) { setStatus(t, false); }
		}).then(function () {
			window.location.reload();
		}, function (err) {
			setStatus(err.message, true);
			start.disabled = false;
		});
	});
})();
