# Rechtsrahmen STYRIXA Shop (Österreich + EU) – Anforderungskatalog

Stand: 25.11.2026 · Grundlage für Phase 5 (`docs/UMSETZUNGSPLAN.md`). Jede Anforderung hat eine ID (`R-…`), auf die Sessions und Tests verweisen.

**Wichtig:** Diese Liste übersetzt die öffentlichen Informationen von WKO und USP in Software-Anforderungen. Sie ist **keine Rechtsberatung**. Inhalte (AGB, Datenschutzerklärung, Steuerfragen) verantwortet der Shop-Betreiber; das CMS stellt die Funktionen bereit und erzwingt, dass Pflichtangaben vorhanden sind.

**Rahmen (Entscheidungen 25.11.2026):** Verkauf an Verbraucher in **Österreich und der EU** · Produktarten **Waren, Downloads, Dienstleistungen** · **altersbeschränkte Waren** (z. B. Wein) möglich · gesetzliche Muster (Widerrufsbelehrung, Muster-Widerrufsformular) **vorbefüllt**.

## 1. Impressum und Offenlegung

| ID | Anforderung | Umsetzung im CMS |
|---|---|---|
| R-IMP-1 | Pflichtangaben nach § 5 ECG, § 14 UGB, § 63 GewO und Offenlegung nach § 25 MedienG | Settings „Betreiberangaben“ (Firma, Rechtsform, Anschrift der Niederlassung, Telefon, E-Mail, Firmenbuchnummer/-gericht, UID, GISA-Zahl, Kammer/Berufsverband, Aufsichtsbehörde, Medieninhaber, Unternehmensgegenstand); daraus erzeugte Seite „Impressum“, direkt auf der eigenen Website (nicht nur verlinkt) |
| R-IMP-2 | Impressum von jeder Seite erreichbar | fester Footer-Link in jedem Theme (Pflicht-Hook) |
| R-IMP-3 | Firmenangaben auch in geschäftlichen E-Mails (UGB) | Footer aller Shop-Mails aus R-IMP-1 |
| R-IMP-4 | **Kein** Hinweis/Link auf die EU-OS-Plattform (seit 20.07.2025 abgeschafft) | Test: kein `ec.europa.eu/consumers/odr` in Templates, Mails, Mustertexten |
| R-IMP-5 | Alternative Streitbeilegung (AStG): Angabe, ob der Betreiber an AS-Verfahren teilnimmt, plus E-Mail für Beschwerden | Settings-Feld; Ausgabe in Impressum und AGB-Hinweis |

## 2. Informationen vor der Bestellung (§ 4 FAGG)

| ID | Anforderung | Umsetzung |
|---|---|---|
| R-INF-1 | wesentliche Merkmale, Identität, Anschrift, **Telefon und E-Mail** | Produktseite + Pflichtseiten + Impressum |
| R-INF-2 | Gesamtpreis inkl. Steuern und aller Nebenkosten | Preisanzeige (R-PRE), Versandkosten vor dem Checkout |
| R-INF-3 | Zahlungs-, Liefer- und Leistungsbedingungen, **Lieferfrist** | Seite „Versand & Zahlung“, Lieferzeit je Produkt |
| R-INF-4 | Widerrufsbedingungen, Frist, Verfahren, **Muster-Widerrufsformular**, Hinweis auf Bestehen und Platzierung der **Online-Widerrufsfunktion** (§ 13a) | vorbefüllte Seite „Widerrufsbelehrung“ inkl. Formular, Textbaustein zum Widerrufsbutton |
| R-INF-5 | ggf. Rücksendekosten; Hinweis, wenn Ware nicht per Post rücksendbar ist (Höhe angeben) | Setting „Rücksendekosten trägt“; Produktfeld „Speditionsware“ mit Kostenangabe |
| R-INF-6 | ggf. Hinweis auf Verlust des Widerrufsrechts (Downloads, Dienstleistungen, ausgenommene Waren) | Produkttyp-abhängiger Hinweis |
| R-INF-7 | Gewährleistung/Garantien mit **EU-Gewährleistungslabel** (seit 27.09.2026, DVO (EU) 2025/1960; § 4 Abs 1 Z 12, 12a FAGG) | siehe R-LBL |
| R-INF-8 | AGB vor der Bestellung abrufbar und akzeptiert | Pflichtseite „AGB“ (Inhalt vom Betreiber), Checkbox im Checkout |

## 3. Preisangaben

| ID | Anforderung | Umsetzung |
|---|---|---|
| R-PRE-1 | Bruttopreise „inkl. USt.“, Versandkosten-Hinweis mit Link | Produkt-, Katalog-, Warenkorbseite |
| R-PRE-2 | Grundpreis (Preis je Mengeneinheit) wo vorgeschrieben | Produktfelder Inhalt/Einheit (vorhanden), Anzeige |
| R-PRE-3 | Preisermäßigung mit niedrigstem Preis der letzten **30 Tage** (§ 9a PrAG) | Preisverlauf (vorhanden), Anzeige neben Streichpreis |
| R-PRE-4 | Kleinunternehmerregelung: keine USt ausweisen, Hinweis auf Steuerbefreiung | Setting; Preis- und Rechnungstexte passen sich an |
| R-PRE-5 | keine Zuschläge für Zahlungsmittel über den Kosten (ZaDiG 2018) | Shop bietet **keine** Zahlungsgebühren an |
| R-PRE-6 | Bewertungen: Angabe, ob echte Käufer sichergestellt sind (UWG) | v1 ohne Bewertungsfunktion; bei späterem Ausbau Pflicht |

## 4. Bestellvorgang (§ 8 FAGG, KSchG)

| ID | Anforderung | Umsetzung |
|---|---|---|
| R-BST-1 | Lieferbeschränkungen und akzeptierte Zahlungsmittel **spätestens zu Beginn** des Bestellvorgangs | Hinweisblock im Warenkorb (Liefergebiete, Zahlungsarten) |
| R-BST-2 | **unmittelbar vor** dem Bestellbutton: Merkmale, Gesamtpreis inkl. Steuern und Versand, ggf. Laufzeit – hervorgehoben, **nicht nur per Link** | Zusammenfassung direkt über dem Button |
| R-BST-3 | Button gut lesbar **„zahlungspflichtig bestellen“** – sonst ist der Verbraucher nicht gebunden | fester Text, Test |
| R-BST-4 | **keine vorausgewählten** Zusatzleistungen; Extrazahlungen nur per ausdrücklicher Zustimmung (§ 6c KSchG) | alle Checkboxen leer; Test |
| R-BST-5 | Geoblocking-VO: Kunden aus allen EU-Ländern dürfen zu gleichen Bedingungen bestellen; Einschränkung nur der **Lieferadresse** zulässig | Rechnungsadresse EU-weit; Liefergebiete konfigurierbar; keine Texte „nur für Kunden aus Österreich“ |
| R-BST-6 | Downloads: ausdrückliche Zustimmung zur sofortigen Bereitstellung **und** Bestätigung der Kenntnis vom Verlust des Widerrufsrechts | zwei aktive Checkboxen nur bei Downloads, Zeitstempel speichern |
| R-BST-7 | Dienstleistungen mit Beginn in der Widerrufsfrist: ausdrückliches Verlangen **und** Kenntnisbestätigung (anteilige Zahlung bei Widerruf) | zwei aktive Checkboxen, Zeitstempel |
| R-BST-8 | Altersbeschränkte Waren: Altersbestätigung des Käufers | Checkbox „mindestens 16/18 Jahre“ je höchster Altersgrenze im Warenkorb, Vermerk auf Lieferschein |
| R-BST-9 | Kontakt zu bestehenden Verträgen nicht über Mehrwertnummer (§ 6b KSchG) | Hinweis in den Settings, Validierung der Telefonnummer (keine 0900/0930/0939) |

## 5. Bestätigung auf dauerhaftem Datenträger (§ 7 Abs 3 FAGG)

| ID | Anforderung | Umsetzung |
|---|---|---|
| R-BEST-1 | Vertragsbestätigung mit **allen vorvertraglichen Informationen**, spätestens bei Lieferung | Bestellmail mit PDF „Vertragsunterlagen“ (Bestellung, AGB, Widerrufsbelehrung, Muster-Widerrufsformular, Labels) |
| R-BEST-2 | bei Downloads/Dienstleistungen die abgegebenen Zustimmungen in der Bestätigung | Abschnitt in Mail + PDF |
| R-BEST-3 | Gewährleistungs- (und ggf. Garantie-)Label auch in der Bestellmail | eingebettet gemäß EU-Leitlinien |

## 6. Widerrufsrecht und Widerrufsbutton (§§ 11, 13a FAGG; gilt seit 01.10.2026, bei Ausrichtung auf andere EU-Länder seit 19.06.2026)

| ID | Anforderung | Umsetzung |
|---|---|---|
| R-WID-1 | Funktion **„Vertrag widerrufen“**, hervorgehoben, leicht auffindbar, während der ganzen Frist verfügbar, auf allen Geräten; ein unauffälliger Footer-Link reicht nicht | eigener, hervorgehobener Button im Footer jedes Themes (Pflicht-Hook) + Link in jeder Bestellmail + im Kundenkonto |
| R-WID-2 | **ohne Login** nutzbar (auch Gastbestellungen) | öffentliches Formular |
| R-WID-3 | Pflichtfelder **nur**: Name, Vertragsidentifikation (Bestellnummer), Kommunikationsmittel für die Bestätigung (E-Mail); optional Auswahl der Artikel (Teilwiderruf) und freiwilliger Grund | Formular, Test der Pflichtfelder |
| R-WID-4 | Bestätigungsschaltfläche **„Widerruf bestätigen“** | fester Text |
| R-WID-5 | **unverzügliche Eingangsbestätigung** auf dauerhaftem Datenträger mit Erklärung, **Datum und Uhrzeit** – neutral formuliert, auch wenn der Widerruf später unwirksam ist | Mail sofort (nicht als später Job), Anzeige auf der Seite, Eintrag in der DB |
| R-WID-6 | Widerrufsfrist 14 Tage; fehlt Button oder Belehrung, verlängert sie sich um 12 Monate | vorbefüllte Belehrung + Readiness-Check (R-SYS-1) |
| R-WID-7 | Rückzahlung, Rücksendekosten, Prüfung | Admin-Posteingang „Widerrufe“ mit Status (eingegangen → berechtigt/unwirksam → erstattet), Mitteilung an Kunden, Erstattung per Klick (Stripe) |

## 7. Gewährleistungs- und Garantielabel (DVO (EU) 2025/1960, seit 27.09.2026)

| ID | Anforderung | Umsetzung |
|---|---|---|
| R-LBL-1 | Gewährleistungslabel: unveränderte **farbige** EU-Grafik mit QR-Code, in allen Shop-Sprachen, an zentraler gut sichtbarer Stelle | offizielle deutsche Fassung vendort (Quelle: EU-Kommission), Ausgabe auf Produkt- und Infoseite; Variante für Gebrauchtwaren |
| R-LBL-2 | Garantielabel nur bei kostenloser Herstellergarantie > 2 Jahre (vom Hersteller mitgeteilt); anpassbar nur Dauer, Marke, Modell | Produktfelder; Ausgabe auf Produktseite **und** unmittelbar vor der Bestellung |
| R-LBL-3 | beide Labels in der Bestellmail | siehe R-BEST-3 |
| R-LBL-4 | Downloads/Dienstleistungen: Gewährleistungsinformation ohne Label | Textbaustein |

## 8. Lieferung, Steuern, Rechnung

| ID | Anforderung | Umsetzung |
|---|---|---|
| R-LIE-1 | Lieferung ohne Vereinbarung binnen **30 Tagen** (§ 7a KSchG); Gefahr geht erst mit Übergabe an den Verbraucher über (§ 7b KSchG) | Lieferzeit je Produkt (max. 30 Tage ohne Vereinbarung), AGB-Hinweis vom Betreiber |
| R-STE-1 | EU-Fernverkauf: über **10.000 €** EU-weit Besteuerung im Bestimmungsland (OSS) | Setting „OSS aktiv“; Steuersätze je EU-Land mit Gültigkeit; Satz nach Lieferland |
| R-STE-2 | OSS-Meldung | Export je Quartal (Land × Steuersatz × Netto/USt) als CSV |
| R-RE-1 | Rechnung mit Pflichtangaben nach UStG, fortlaufende Nummer | Session 5.8 |
| R-RE-2 | Aufbewahrung 7 Jahre (BAO) | Kundendaten anonymisieren, Rechnungen behalten (R-DSG-3) |

## 9. Datenschutz und Barrierefreiheit

| ID | Anforderung | Umsetzung |
|---|---|---|
| R-DSG-1 | DSGVO/TKG: keine nicht notwendigen Cookies ohne Einwilligung | Shop nutzt nur ein technisch notwendiges Warenkorb-Cookie, kein Tracking → kein Banner |
| R-DSG-2 | Datenschutzerklärung | Pflichtseite (Inhalt Betreiber), Textbausteine zu Stripe und Versand |
| R-DSG-3 | Auskunft und Löschung | Admin: Kundendaten exportieren (JSON), anonymisieren unter Wahrung der 7-Jahres-Aufbewahrung |
| R-BAR-1 | BaFG: Webshops müssen barrierefrei sein, **außer** der Betreiber ist ein Kleinstunternehmen (< 10 Mitarbeiter, ≤ 2 Mio. € Umsatz oder Bilanzsumme) | Checkout/Shop-Seiten immer WCAG 2.1 AA (Standard für alle); Setting „Kleinstunternehmen: nein“ → Pflichtseite „Barrierefreiheit“ |

## 10. System-Absicherung

| ID | Anforderung | Umsetzung |
|---|---|---|
| R-SYS-1 | Kein Verkauf, solange Pflichtangaben fehlen | **Shop-Bereitschaftsprüfung**: Checkout gesperrt (Hinweis im Admin), bis Betreiberangaben, Pflichtseiten (Impressum, AGB, Widerrufsbelehrung, Datenschutz, Versand & Zahlung, ggf. Barrierefreiheit), Liefergebiete, Steuer-Setup und mindestens eine Zahlungsart vollständig sind |
| R-SYS-2 | Änderungen an Rechtstexten nachvollziehbar | Versionen der Pflichtseiten; jede Bestellung speichert die Versions-IDs, die der Kunde gesehen hat |
| R-SYS-3 | Hinweis im Admin | „Rechts- und Steuerwerte sind vom Betreiber zu prüfen; keine Rechtsberatung“ |

## Betreiber-Checkliste (nicht Software – vor dem Livegang vom Shop-Betreiber zu erledigen)

- Gewerbeberechtigung, GISA-Zahl, Firmenbuch, UID
- AGB und Datenschutzerklärung erstellen bzw. prüfen lassen (z. B. WKO-Muster)
- Verpackungen lizenzieren (Verpackungsverordnung; ab 12.08.2026 zusätzlich EU-Verpackungsverordnung PPWR); ggf. Elektro-/Batterie-Pflichten
- Steuern mit Steuerberatung klären: OSS-Registrierung, Kleinunternehmerregelung, Registrierkassen-/Belegpflicht
- Jugendschutz: Altersgrenzen je Produkt prüfen, Versand mit Altersprüfung beim Zusteller beauftragen
- BaFG: Kleinstunternehmen ja/nein prüfen
- Stripe-Konto, Zahlarten (EPS, Klarna, PayPal) freischalten

## Quellen

- USP: [Rechtliche Pflichten für Onlineshops](https://www.usp.gv.at/themen/betrieb-und-umwelt/laufender-betrieb/weitere-informationen-laufender-betrieb/e-commerce-kompetenz/anleitungen-um-einen-online-shop-aufzubauen/rechtliche-pflichten-fuer-onlineshops.html)
- WKO: [Webshops – die wesentlichen Bestimmungen (Verbraucherrechte)](https://www.wko.at/internetrecht/webshops-die-wesentlichen-bestimmungen-verbraucherrechte)
- WKO NÖ: [Widerrufsbutton und Gewährleistungs-/Garantielabel](https://www.wko.at/noe/e-commerce/e-commerce-rechtsfrage-22-widerrufsbutton)
- WKO: [Widerrufsbutton im Webshop](https://www.wko.at/internetrecht/e-commerce-widerrufsbutton-webshop) · RIS: [BGBl. I Nr. 59/2026 (VerbRÄG 2026)](https://ris.bka.gv.at/Dokumente/BgblAuth/BGBLA_2026_I_59/BGBLA_2026_I_59.pdf)
- WKO: [Alternative Streitbeilegung / OS-Plattform](https://www.wko.at/internetrecht/alternative-streitbeilegung)
- WKO: [Impressum und Pflichtangaben im E-Commerce](https://www.wko.at/internetrecht/informationspflichten-nach-dem-e-commerce-gesetz--dem-unte)
- WKO: [Barrierefreiheitsgesetz im E-Commerce – FAQ](https://www.wko.at/internetrecht/barrierefreiheitsgesetz-im-e-commerce-faq)
- WKO NÖ: [Umweltrechtliche Aspekte im Versandhandel](https://www.wko.at/noe/e-commerce/umweltrechtliche-aspekte-im-versandhandel)
