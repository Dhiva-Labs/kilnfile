package main

import (
	"fmt"
	"strings"
)

const (
	nsRel   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	nsPkgRl = "http://schemas.openxmlformats.org/package/2006/relationships"
	nsCT    = "http://schemas.openxmlformats.org/package/2006/content-types"
	nsW     = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	nsA     = "http://schemas.openxmlformats.org/drawingml/2006/main"
	xmlDecl = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"

	ctWordMain    = "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"
	ctWordMacro   = "application/vnd.ms-word.document.macroEnabled.main+xml"
	ctWordStyles  = "application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"
	ctWordSetting = "application/vnd.openxmlformats-officedocument.wordprocessingml.settings+xml"
)

type rel struct {
	id, typ, target string
	external        bool
}

func relsXML(rels []rel) []byte {
	var b strings.Builder
	b.WriteString(xmlDecl + `<Relationships xmlns="` + nsPkgRl + `">`)
	for _, r := range rels {
		mode := ""
		if r.external {
			mode = ` TargetMode="External"`
		}
		fmt.Fprintf(&b, `<Relationship Id="%s" Type="%s" Target="%s"%s/>`, r.id, r.typ, r.target, mode)
	}
	b.WriteString(`</Relationships>`)
	return []byte(b.String())
}

// pkg assembles an OPC package: parts keep insertion order, [Content_Types].xml
// and _rels/.rels are generated.
type pkg struct {
	defaults  [][2]string
	overrides [][2]string
	rels      map[string][]rel // part path whose rels file is emitted ("" = package root)
	relOrder  []string
	parts     []zipFile
}

func newPkg() *pkg {
	p := &pkg{rels: map[string][]rel{}}
	p.defaults = [][2]string{{"rels", "application/vnd.openxmlformats-package.relationships+xml"}, {"xml", "application/xml"}}
	return p
}

func (p *pkg) part(name, ct string, data []byte) {
	if ct != "" {
		p.overrides = append(p.overrides, [2]string{"/" + name, ct})
	}
	p.parts = append(p.parts, zipFile{name: name, data: data})
}

func (p *pkg) def(ext, ct string) { p.defaults = append(p.defaults, [2]string{ext, ct}) }

func (p *pkg) rel(owner string, r rel) {
	if _, ok := p.rels[owner]; !ok {
		p.relOrder = append(p.relOrder, owner)
	}
	p.rels[owner] = append(p.rels[owner], r)
}

func relsPath(owner string) string {
	if owner == "" {
		return "_rels/.rels"
	}
	i := strings.LastIndex(owner, "/")
	return owner[:i+1] + "_rels/" + owner[i+1:] + ".rels"
}

func (p *pkg) build() []byte {
	var ct strings.Builder
	ct.WriteString(xmlDecl + `<Types xmlns="` + nsCT + `">`)
	for _, d := range p.defaults {
		fmt.Fprintf(&ct, `<Default Extension="%s" ContentType="%s"/>`, d[0], d[1])
	}
	for _, o := range p.overrides {
		fmt.Fprintf(&ct, `<Override PartName="%s" ContentType="%s"/>`, o[0], o[1])
	}
	ct.WriteString(`</Types>`)
	files := []zipFile{{name: "[Content_Types].xml", data: []byte(ct.String())}}
	for _, owner := range p.relOrder {
		if owner == "" {
			files = append(files, zipFile{name: relsPath(owner), data: relsXML(p.rels[owner])})
		}
	}
	files = append(files, p.parts...)
	for _, owner := range p.relOrder {
		if owner != "" {
			files = append(files, zipFile{name: relsPath(owner), data: relsXML(p.rels[owner])})
		}
	}
	return zipBytes(files)
}

// ---- Word ----

const wordNS = `xmlns:w="` + nsW + `" xmlns:r="` + nsRel + `" xmlns:v="urn:schemas-microsoft-com:vml" ` +
	`xmlns:o="urn:schemas-microsoft-com:office:office" xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" ` +
	`xmlns:a="` + nsA + `" xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"`

func para(text string) string { return `<w:p><w:r><w:t>` + text + `</w:t></w:r></w:p>` }

func wordDoc(prolog, body string) []byte {
	return []byte(xmlDecl + prolog + `<w:document ` + wordNS + `><w:body>` + body +
		`<w:sectPr><w:pgSz w:w="12240" w:h="15840"/></w:sectPr></w:body></w:document>`)
}

// newDocx returns a minimal valid DOCX package. mainCT is the content type of
// word/document.xml (macro-enabled for DOCM).
func newDocx(mainCT, prolog, body string) *pkg {
	p := newPkg()
	p.rel("", rel{id: "rId1", typ: nsRel + "/officeDocument", target: "word/document.xml"})
	p.part("word/document.xml", mainCT, wordDoc(prolog, body))
	p.part("word/styles.xml", ctWordStyles, []byte(xmlDecl+`<w:styles xmlns:w="`+nsW+`"><w:docDefaults><w:rPrDefault><w:rPr><w:sz w:val="22"/></w:rPr></w:rPrDefault></w:docDefaults></w:styles>`))
	p.rel("word/document.xml", rel{id: "rId1", typ: nsRel + "/styles", target: "styles.xml"})
	return p
}

var dummyVBA = []byte("KILNFILE-DUMMY-VBA-PROJECT: not real VBA, inert test bytes only.\x00\x01\x02\x03")

func addVBA(p *pkg) {
	p.part("word/vbaProject.bin", "application/vnd.ms-office.vbaProject", dummyVBA)
	p.rel("word/document.xml", rel{id: "rId50", typ: "http://schemas.microsoft.com/office/2006/relationships/vbaProject", target: "vbaProject.bin"})
}

// ---- Excel ----

func newXlsx() *pkg {
	p := newPkg()
	p.rel("", rel{id: "rId1", typ: nsRel + "/officeDocument", target: "xl/workbook.xml"})
	p.part("xl/workbook.xml", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml",
		[]byte(xmlDecl+`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="`+nsRel+`"><sheets><sheet name="Sheet1" sheetId="1" r:id="rId1"/></sheets></workbook>`))
	p.rel("xl/workbook.xml", rel{id: "rId1", typ: nsRel + "/worksheet", target: "worksheets/sheet1.xml"})
	p.part("xl/worksheets/sheet1.xml", "application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml",
		[]byte(xmlDecl+`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>Hello from kilnfile</t></is></c><c r="B1"><v>42</v></c></row></sheetData></worksheet>`))
	return p
}

// ---- PowerPoint ----

const (
	nsP     = "http://schemas.openxmlformats.org/presentationml/2006/main"
	pptNS   = `xmlns:a="` + nsA + `" xmlns:r="` + nsRel + `" xmlns:p="` + nsP + `"`
	spTree0 = `<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr/>`
)

func themeXML() []byte {
	fills := strings.Repeat(`<a:solidFill><a:schemeClr val="phClr"/></a:solidFill>`, 3)
	lines := strings.Repeat(`<a:ln w="9525"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln>`, 3)
	effects := strings.Repeat(`<a:effectStyle><a:effectLst/></a:effectStyle>`, 3)
	accents := ""
	for i, c := range []string{"4472C4", "ED7D31", "A5A5A5", "FFC000", "5B9BD5", "70AD47"} {
		accents += fmt.Sprintf(`<a:accent%d><a:srgbClr val="%s"/></a:accent%d>`, i+1, c, i+1)
	}
	font := `<a:latin typeface="Calibri"/><a:ea typeface=""/><a:cs typeface=""/>`
	return []byte(xmlDecl + `<a:theme xmlns:a="` + nsA + `" name="Kiln"><a:themeElements><a:clrScheme name="Kiln">` +
		`<a:dk1><a:sysClr val="windowText" lastClr="000000"/></a:dk1><a:lt1><a:sysClr val="window" lastClr="FFFFFF"/></a:lt1>` +
		`<a:dk2><a:srgbClr val="1F2937"/></a:dk2><a:lt2><a:srgbClr val="F3F4F6"/></a:lt2>` + accents +
		`<a:hlink><a:srgbClr val="0563C1"/></a:hlink><a:folHlink><a:srgbClr val="954F72"/></a:folHlink></a:clrScheme>` +
		`<a:fontScheme name="Kiln"><a:majorFont>` + font + `</a:majorFont><a:minorFont>` + font + `</a:minorFont></a:fontScheme>` +
		`<a:fmtScheme name="Kiln"><a:fillStyleLst>` + fills + `</a:fillStyleLst><a:lnStyleLst>` + lines +
		`</a:lnStyleLst><a:effectStyleLst>` + effects + `</a:effectStyleLst><a:bgFillStyleLst>` + fills +
		`</a:bgFillStyleLst></a:fmtScheme></a:themeElements></a:theme>`)
}

func newPptx() *pkg {
	p := newPkg()
	p.rel("", rel{id: "rId1", typ: nsRel + "/officeDocument", target: "ppt/presentation.xml"})
	p.part("ppt/presentation.xml", "application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml",
		[]byte(xmlDecl+`<p:presentation `+pptNS+`><p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="rId1"/></p:sldMasterIdLst><p:sldIdLst><p:sldId id="256" r:id="rId2"/></p:sldIdLst><p:sldSz cx="9144000" cy="6858000"/><p:notesSz cx="6858000" cy="9144000"/></p:presentation>`))
	p.rel("ppt/presentation.xml", rel{id: "rId1", typ: nsRel + "/slideMaster", target: "slideMasters/slideMaster1.xml"})
	p.rel("ppt/presentation.xml", rel{id: "rId2", typ: nsRel + "/slide", target: "slides/slide1.xml"})
	p.rel("ppt/presentation.xml", rel{id: "rId3", typ: nsRel + "/theme", target: "theme/theme1.xml"})
	p.part("ppt/slideMasters/slideMaster1.xml", "application/vnd.openxmlformats-officedocument.presentationml.slideMaster+xml",
		[]byte(xmlDecl+`<p:sldMaster `+pptNS+`><p:cSld><p:spTree>`+spTree0+`</p:spTree></p:cSld><p:clrMap bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/><p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="rId1"/></p:sldLayoutIdLst></p:sldMaster>`))
	p.rel("ppt/slideMasters/slideMaster1.xml", rel{id: "rId1", typ: nsRel + "/slideLayout", target: "../slideLayouts/slideLayout1.xml"})
	p.rel("ppt/slideMasters/slideMaster1.xml", rel{id: "rId2", typ: nsRel + "/theme", target: "../theme/theme1.xml"})
	p.part("ppt/slideLayouts/slideLayout1.xml", "application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml",
		[]byte(xmlDecl+`<p:sldLayout `+pptNS+` type="blank"><p:cSld name="Blank"><p:spTree>`+spTree0+`</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sldLayout>`))
	p.rel("ppt/slideLayouts/slideLayout1.xml", rel{id: "rId1", typ: nsRel + "/slideMaster", target: "../slideMasters/slideMaster1.xml"})
	p.part("ppt/slides/slide1.xml", "application/vnd.openxmlformats-officedocument.presentationml.slide+xml",
		[]byte(xmlDecl+`<p:sld `+pptNS+`><p:cSld><p:spTree>`+spTree0+`<p:sp><p:nvSpPr><p:cNvPr id="2" name="Text 1"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="914400" y="914400"/><a:ext cx="4572000" cy="914400"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr><p:txBody><a:bodyPr/><a:lstStyle/><a:p><a:r><a:t>Hello from kilnfile</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sld>`))
	p.rel("ppt/slides/slide1.xml", rel{id: "rId1", typ: nsRel + "/slideLayout", target: "../slideLayouts/slideLayout1.xml"})
	p.part("ppt/theme/theme1.xml", "application/vnd.openxmlformats-officedocument.theme+xml", themeXML())
	return p
}

func genOOXML(c *corpus) error {
	const bodyText = "Hello from kilnfile"
	keepDoc := []string{"word/document.xml"}

	// docx variants
	plain := func() *pkg { return newDocx(ctWordMain, "", para(bodyText)) }

	macro := newDocx(ctWordMacro, "", para(bodyText))
	addVBA(macro)

	tmpl := plain()
	tmpl.part("word/settings.xml", ctWordSetting, []byte(xmlDecl+`<w:settings xmlns:w="`+nsW+`" xmlns:r="`+nsRel+`"><w:attachedTemplate r:id="rId1"/></w:settings>`))
	tmpl.rel("word/document.xml", rel{id: "rId2", typ: nsRel + "/settings", target: "settings.xml"})
	tmpl.rel("word/settings.xml", rel{id: "rId1", typ: nsRel + "/attachedTemplate", target: "http://example.invalid/t.dotm", external: true})

	dde := newDocx(ctWordMain, "", `<w:p><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> DDEAUTO c:\\windows\\system32\\cmd.exe "/k echo test" </w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>`+bodyText+`</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r></w:p>`)

	activex := newDocx(ctWordMain, "", para(bodyText)+`<w:p><w:r><w:object w:dxaOrig="1440" w:dyaOrig="1440"><w:control r:id="rId60" w:name="CommandButton1" w:shapeid="_x0000_i1025"/></w:object></w:r></w:p>`)
	activex.part("word/activeX/activeX1.xml", "application/vnd.ms-office.activeX+xml",
		[]byte(xmlDecl+`<ax:ocx xmlns:ax="http://schemas.microsoft.com/office/2006/activeX" xmlns:r="`+nsRel+`" ax:classid="{D7053240-CE69-11CD-A777-00DD01143C57}" ax:persistence="persistStorage" r:id="rId1"/>`))
	activex.part("word/activeX/activeX1.bin", "", []byte("KILNFILE-DUMMY-ACTIVEX-BINARY: inert test bytes only."))
	activex.def("bin", "application/vnd.ms-office.activeX")
	activex.rel("word/document.xml", rel{id: "rId60", typ: nsRel + "/control", target: "activeX/activeX1.xml"})
	activex.rel("word/activeX/activeX1.xml", rel{id: "rId1", typ: "http://schemas.microsoft.com/office/2006/relationships/activeXControlBinary", target: "activeX1.bin"})

	ole := newDocx(ctWordMain, "", para(bodyText)+`<w:p><w:r><w:object><o:OLEObject Type="Embed" ProgID="Package" ShapeID="_x0000_i1025" DrawAspect="Content" ObjectID="_1" r:id="rId70"/></w:object></w:r></w:p>`)
	ole.part("word/embeddings/oleObject1.bin", "", []byte("KILNFILE-DUMMY-OLE-OBJECT: inert test bytes only."))
	ole.def("bin", "application/vnd.openxmlformats-officedocument.oleObject")
	ole.rel("word/document.xml", rel{id: "rId70", typ: nsRel + "/oleObject", target: "embeddings/oleObject1.bin"})

	cui := newDocx(ctWordMacro, "", para(bodyText))
	addVBA(cui)
	cui.part("customUI/customUI.xml", "", []byte(xmlDecl+`<customUI xmlns="http://schemas.microsoft.com/office/2006/01/customui"><ribbon><tabs><tab id="kilnTab" label="Kiln"><group id="kilnGroup" label="Kiln"><button id="kilnButton" label="Run" onAction="DummyMacro"/></group></tab></tabs></ribbon></customUI>`))
	cui.rel("", rel{id: "rId9", typ: "http://schemas.microsoft.com/office/2007/relationships/ui/extensibility", target: "customUI/customUI.xml"})

	xxe := newDocx(ctWordMain, "<!DOCTYPE w:document [\n<!ENTITY xxe SYSTEM \"file:///etc/passwd\">\n]>\n", para(bodyText+" &xxe;"))

	img := newDocx(ctWordMain, "", para(bodyText)+`<w:p><w:r><w:drawing><wp:inline><wp:extent cx="95250" cy="95250"/><wp:docPr id="1" name="Picture 1"/><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic><pic:nvPicPr><pic:cNvPr id="0" name="image1.png"/><pic:cNvPicPr/></pic:nvPicPr><pic:blipFill><a:blip r:embed="rId80"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill><pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="95250" cy="95250"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`)
	img.def("png", "image/png")
	img.part("word/media/image1.png", "", pngBytes(16, 16))
	img.rel("word/document.xml", rel{id: "rId80", typ: nsRel + "/image", target: "media/image1.png"})

	with := func(e Expected, keep ...string) Expected { e.MustContain = keep; return e }
	samples := []struct {
		path, desc string
		p          *pkg
		exp        Expected
	}{
		{"ooxml/benign.docx", "Benign minimal DOCX.", plain(), with(ok, keepDoc...)},
		{"ooxml/benign.xlsx", "Benign minimal XLSX.", newXlsx(), with(ok, "xl/workbook.xml")},
		{"ooxml/benign.pptx", "Benign minimal PPTX (one slide, master, layout, theme).", newPptx(), with(ok, "ppt/slides/slide1.xml")},
		{"ooxml/macro.docm", "Macro-enabled DOCM with a dummy word/vbaProject.bin, the macro content type and relationship. Output must be a plain DOCX.",
			macro, with(sanit([]string{actVBA}, "word/vbaProject.bin", "vbaProject", "macroEnabled"), keepDoc...)},
		{"ooxml/external_template.docx", "DOCX whose settings.xml.rels has an attachedTemplate relationship, TargetMode External, to http://example.invalid/t.dotm.",
			tmpl, with(sanit([]string{actExtTemplate}, "example.invalid", "attachedTemplate"), "word/settings.xml")},
		{"ooxml/ddeauto.docx", "DOCX with a DDEAUTO field in document.xml (inert command string).",
			dde, with(sanit([]string{actDDE}, "DDEAUTO", "cmd.exe"), keepDoc...)},
		{"ooxml/activex.docx", "DOCX with an ActiveX control part (dummy bytes) and a w:control reference.",
			activex, with(sanit([]string{actActiveX}, "word/activeX/", "activeX1"), keepDoc...)},
		{"ooxml/ole_object.docx", "DOCX with an oleObject embedding (dummy bytes) and an o:OLEObject reference.",
			ole, with(sanit([]string{actOLE}, "oleObject", "word/embeddings/"), keepDoc...)},
		{"ooxml/customui.docm", "DOCM with customUI/customUI.xml whose button names a macro, plus a dummy vbaProject.bin.",
			cui, with(sanit([]string{actCustomUI, actVBA}, "customUI", "vbaProject", "DummyMacro"), keepDoc...)},
		{"ooxml/xxe_doctype.docx", "DOCX whose document.xml has a DOCTYPE with an internal-subset external entity to file:///etc/passwd, referenced in the body.",
			xxe, with(sanit([]string{actDTD}, "DOCTYPE", "ENTITY", "file:///etc/passwd"), keepDoc...)},
		{"ooxml/image.docx", "Benign DOCX with an embedded PNG image part; the image must survive (re-encoded).",
			img, with(ok, "word/media/image1.png")},
	}
	for _, s := range samples {
		if err := c.add(s.path, "ooxml", s.desc, s.p.build(), s.exp); err != nil {
			return err
		}
	}
	return nil
}
