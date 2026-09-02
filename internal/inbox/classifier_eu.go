package inbox

import "regexp"

// This file extends the keyword classifier for the EU/GDPR use this fork is
// built around. The upstream patterns in classifier.go are English and
// CCPA-flavoured, so replies that talk about "erasure" / "Article 17" / a
// "data protection officer", and replies in German or French (the two largest
// EU markets, and where brokers reply in the local language), were all landing
// as `unknown`.
//
// init() appends to the package-level pattern slices, so both ClassifyResponse
// and ClassifyBySubjectOnly pick these up. Non-English patterns are kept
// high-signal to avoid false positives against English text.

func init() {
	successPatterns = append(successPatterns,
		// GDPR English
		regexp.MustCompile(`(?i)(personal\s+data|information)\s+(has|have)\s+been\s+eras(ed|e)`),
		regexp.MustCompile(`(?i)erasure\s+(request\s+)?(has\s+been\s+)?(completed|actioned|fulfilled|carried\s+out)`),
		regexp.MustCompile(`(?i)right\s+to\s+be\s+forgotten.{0,40}(complete|actioned|fulfilled|honou?red)`),
		regexp.MustCompile(`(?i)we\s+have\s+(now\s+)?(erased|deleted)\s+(all\s+)?(the\s+)?(your\s+)?(personal\s+)?(data|information)`),
		regexp.MustCompile(`(?i)(you\s+have|your\s+data\s+has)\s+been\s+removed\s+from\s+(all\s+)?our\s+(systems|databases|records)`),
		// German
		regexp.MustCompile(`(?i)(ihre\s+)?(personenbezogenen\s+)?daten\s+(wurden|haben\s+wir)\s+(gelöscht|entfernt)`),
		regexp.MustCompile(`(?i)löschung\s+(ihrer\s+daten\s+)?(wurde\s+)?(vorgenommen|durchgeführt|abgeschlossen)`),
		regexp.MustCompile(`(?i)wir\s+haben\s+(ihre|alle\s+ihre)\s+daten\s+gelöscht`),
		// French
		regexp.MustCompile(`(?i)(vos\s+)?données\s+(à\s+caractère\s+personnel\s+)?ont\s+été\s+(supprimées|effacées)`),
		regexp.MustCompile(`(?i)(votre\s+)?demande\s+d['’](effacement|suppression).{0,30}(traitée|effectuée|prise\s+en\s+compte)`),
		regexp.MustCompile(`(?i)nous\s+avons\s+(bien\s+)?supprimé\s+(vos|toutes\s+vos)\s+données`),
	)

	formRequiredPatterns = append(formRequiredPatterns,
		regexp.MustCompile(`(?i)(data\s+subject\s+(access\s+)?request|erasure\s+request)\s+(form|portal)`),
		regexp.MustCompile(`(?i)submit.{0,20}(via|through|using)\s+our\s+(privacy|data\s+protection|gdpr)\s+(portal|cent(re|er)|form)`),
		// German
		regexp.MustCompile(`(?i)(bitte\s+)?(füllen\s+sie|nutzen\s+sie)\s+(das|unser)\s+(online[\s-]?)?formular`),
		regexp.MustCompile(`(?i)über\s+(unser|das)\s+(datenschutz|betroffenenrechte)[\s-]?(portal|formular)`),
		// French
		regexp.MustCompile(`(?i)(veuillez\s+)?(remplir|utiliser)\s+(le|notre)\s+formulaire`),
		regexp.MustCompile(`(?i)via\s+(notre|le)\s+(portail|formulaire)\s+(de\s+)?(protection\s+des\s+données|rgpd|confidentialité)`),
	)

	confirmationPatterns = append(confirmationPatterns,
		// Identity verification under GDPR Art. 12(6)
		regexp.MustCompile(`(?i)(verify|confirm|prove)\s+your\s+identity\s+(before|to\s+(process|proceed|action))`),
		regexp.MustCompile(`(?i)(provide|attach|send|upload)\s+(a\s+)?(copy\s+of\s+)?(your\s+)?(government[\s-]?issued\s+)?(id|identity\s+document|passport|driving\s+licen[sc]e)`),
		regexp.MustCompile(`(?i)proof\s+of\s+(identity|address)\s+(is\s+)?(required|needed)`),
		// German
		regexp.MustCompile(`(?i)(zur\s+)?(überprüfung|bestätigung)\s+ihrer\s+identität`),
		regexp.MustCompile(`(?i)kopie\s+ihres\s+(ausweis|personalausweis|reisepass)`),
		// French
		regexp.MustCompile(`(?i)(vérifier|confirmer|justifier)\s+votre\s+identité`),
		regexp.MustCompile(`(?i)copie\s+de\s+votre\s+(pièce\s+d['’]identité|carte\s+d['’]identité|passeport)`),
	)

	rejectionPatterns = append(rejectionPatterns,
		regexp.MustCompile(`(?i)(do\s+not|don't|does\s+not)\s+(process|hold|store)\s+any\s+personal\s+data\s+(relating|related|concerning|pertaining)\s+to\s+you`),
		regexp.MustCompile(`(?i)no\s+personal\s+data\s+(concerning|relating\s+to|about|pertaining\s+to)\s+you`),
		regexp.MustCompile(`(?i)you\s+are\s+not\s+(present\s+|listed\s+)?in\s+our\s+(database|records|systems)`),
		regexp.MustCompile(`(?i)unable\s+to\s+identify\s+you\s+in\s+our\s+(records|database|systems)`),
		regexp.MustCompile(`(?i)manifestly\s+(unfounded|excessive)`),
		// German
		regexp.MustCompile(`(?i)(wir\s+)?(verarbeiten|speichern|haben)\s+keine\s+(personenbezogenen\s+)?daten\s+(zu|über|von)\s+ihnen`),
		regexp.MustCompile(`(?i)sie\s+sind\s+nicht\s+in\s+unserer\s+datenbank`),
		regexp.MustCompile(`(?i)konnten\s+sie\s+nicht\s+.{0,20}identifizieren`),
		// French
		regexp.MustCompile(`(?i)(nous\s+)?ne\s+(détenons|traitons|possédons)\s+aucune\s+donnée.{0,30}vous\s+concernant`),
		regexp.MustCompile(`(?i)vous\s+n['’]êtes\s+pas\s+(présent|référencé|enregistré).{0,25}(base|fichier|système)`),
	)

	pendingPatterns = append(pendingPatterns,
		regexp.MustCompile(`(?i)acknowledge\s+receipt\s+of\s+your\s+(request|erasure|data\s+subject)`),
		regexp.MustCompile(`(?i)(respond|reply|complete\s+this)\s+within\s+one\s+month`),
		regexp.MustCompile(`(?i)forwarded\s+(your\s+request\s+)?to\s+our\s+(data\s+protection\s+officer|dpo)`),
		regexp.MustCompile(`(?i)our\s+data\s+protection\s+(officer|team)\s+will\s+(review|respond|be\s+in\s+touch)`),
		// German
		regexp.MustCompile(`(?i)(wir\s+haben\s+)?ihre\s+(anfrage|anliegen)\s+erhalten`),
		regexp.MustCompile(`(?i)innerhalb\s+(eines\s+monats|von\s+30\s+tagen)`),
		regexp.MustCompile(`(?i)an\s+(unseren|den)\s+datenschutzbeauftragten\s+weitergeleitet`),
		// French
		regexp.MustCompile(`(?i)nous\s+(avons\s+bien\s+)?(reçu|accusons\s+réception\s+de)\s+votre\s+demande`),
		regexp.MustCompile(`(?i)dans\s+un\s+délai\s+d['’]un\s+mois`),
		regexp.MustCompile(`(?i)transmis\s+(votre\s+demande\s+)?à\s+notre\s+(délégué\s+à\s+la\s+protection\s+des\s+données|dpo)`),
	)

	subjectPendingPatterns = append(subjectPendingPatterns,
		regexp.MustCompile(`(?i)accus[ée]\s+de\s+réception`),
		regexp.MustCompile(`(?i)eingangsbestätigung`),
		regexp.MustCompile(`(?i)acknowledg(e|e?ment)\s+of\s+.{0,20}request`),
	)

	subjectSuccessPatterns = append(subjectSuccessPatterns,
		regexp.MustCompile(`(?i)(gelöscht|löschung\s+abgeschlossen)`),
		regexp.MustCompile(`(?i)(supprimé|effacé|effacement\s+effectué)`),
	)

	subjectRejectionPatterns = append(subjectRejectionPatterns,
		regexp.MustCompile(`(?i)keine\s+daten`),
		regexp.MustCompile(`(?i)aucune\s+donnée`),
		regexp.MustCompile(`(?i)(nicht\s+gefunden|introuvable)`),
	)
}
