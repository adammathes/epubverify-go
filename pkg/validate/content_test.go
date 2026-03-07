package validate

import (
	"strings"
	"testing"

	"github.com/adammathes/epubverify/pkg/report"
)

func TestCheckEpubTypeValid_PlainTypeAttribute(t *testing.T) {
	// A <style type="text/css"> should NOT trigger HTM-015
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head>
  <style type="text/css">body { color: black; }</style>
</head>
<body><p>Hello</p></body>
</html>`

	r := report.NewReport()
	checkEpubTypeValid([]byte(xhtml), "test.xhtml", r)

	for _, m := range r.Messages {
		if m.CheckID == "HTM-015" {
			t.Errorf("plain type attribute should not trigger HTM-015, got: %s", m.Message)
		}
	}
}

func TestCheckEpubTypeValid_NamespacedEpubType(t *testing.T) {
	// A proper epub:type attribute with a valid value should NOT trigger HTM-015
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops">
<head><title>Test</title></head>
<body>
  <nav epub:type="toc"><ol><li>Chapter 1</li></ol></nav>
</body>
</html>`

	r := report.NewReport()
	checkEpubTypeValid([]byte(xhtml), "test.xhtml", r)

	for _, m := range r.Messages {
		if m.CheckID == "HTM-015" {
			t.Errorf("valid epub:type should not trigger HTM-015, got: %s", m.Message)
		}
	}
}

// --- Block-in-Phrasing Content Model Tests ---

func TestCheckBlockInPhrasing_DivInP(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <p>text <div>block inside paragraph</div></p>
</body>
</html>`

	r := report.NewReport()
	checkBlockInPhrasing([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RSC-005 for <div> inside <p>")
	}
}

func TestCheckBlockInPhrasing_DivInH1(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <h1><div>block inside heading</div></h1>
</body>
</html>`

	r := report.NewReport()
	checkBlockInPhrasing([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RSC-005 for <div> inside <h1>")
	}
}

func TestCheckBlockInPhrasing_DivInSpan(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <p><span><div>block inside span</div></span></p>
</body>
</html>`

	r := report.NewReport()
	checkBlockInPhrasing([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RSC-005 for <div> inside <span>")
	}
}

func TestCheckBlockInPhrasing_NoFalsePositive(t *testing.T) {
	// Valid: <div> inside <div> is fine (both are flow content)
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <div><div>nested divs are OK</div></div>
  <section><p>paragraph in section is OK</p></section>
  <p><em><strong>nested inline is OK</strong></em></p>
</body>
</html>`

	r := report.NewReport()
	checkBlockInPhrasing([]byte(xhtml), "test.xhtml", r)

	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			t.Errorf("unexpected RSC-005 for valid nesting: %s", m.Message)
		}
	}
}

func TestCheckBlockInPhrasing_SkipsSVGMathML(t *testing.T) {
	// SVG and MathML content should not trigger block-in-phrasing errors
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <p>
    <svg xmlns="http://www.w3.org/2000/svg"><rect width="10" height="10"/></svg>
    <math xmlns="http://www.w3.org/1998/Math/MathML"><mi>x</mi></math>
  </p>
</body>
</html>`

	r := report.NewReport()
	checkBlockInPhrasing([]byte(xhtml), "test.xhtml", r)

	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			t.Errorf("unexpected RSC-005 for SVG/MathML in <p>: %s", m.Message)
		}
	}
}

// --- Restricted Children Tests ---

func TestCheckRestrictedChildren_DivInUl(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <ul><div>div inside ul</div></ul>
</body>
</html>`

	r := report.NewReport()
	checkRestrictedChildren([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RSC-005 for <div> inside <ul>")
	}
}

func TestCheckRestrictedChildren_DivInTr(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <table><tr><div>div inside tr</div></tr></table>
</body>
</html>`

	r := report.NewReport()
	checkRestrictedChildren([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RSC-005 for <div> inside <tr>")
	}
}

func TestCheckRestrictedChildren_NoFalsePositive(t *testing.T) {
	// Valid nesting: <li> inside <ul>, <td> inside <tr>
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <ul><li>item 1</li><li>item 2</li></ul>
  <ol><li>item 1</li></ol>
  <table><tr><td>cell</td><th>header</th></tr></table>
</body>
</html>`

	r := report.NewReport()
	checkRestrictedChildren([]byte(xhtml), "test.xhtml", r)

	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			t.Errorf("unexpected RSC-005 for valid nesting: %s", m.Message)
		}
	}
}

// --- Void Element Children Tests ---

func TestCheckVoidElementChildren_ChildInBr(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <p>text <br><span>child inside br</span></br> more</p>
</body>
</html>`

	r := report.NewReport()
	checkVoidElementChildren([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RSC-005 for child element inside <br>")
	}
}

func TestCheckVoidElementChildren_ChildInHr(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <hr><p>content inside hr</p></hr>
</body>
</html>`

	r := report.NewReport()
	checkVoidElementChildren([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RSC-005 for child element inside <hr>")
	}
}

func TestCheckVoidElementChildren_NoFalsePositive(t *testing.T) {
	// Self-closing void elements should not trigger errors
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <p>text <br/> more <img src="test.png" alt="test"/> end</p>
  <hr/>
  <p><input type="text"/></p>
</body>
</html>`

	r := report.NewReport()
	checkVoidElementChildren([]byte(xhtml), "test.xhtml", r)

	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			t.Errorf("unexpected RSC-005 for self-closing void elements: %s", m.Message)
		}
	}
}

// --- Table Content Model Tests ---

func TestCheckTableContentModel_PInTable(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <table><p>paragraph directly in table</p></table>
</body>
</html>`

	r := report.NewReport()
	checkTableContentModel([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RSC-005 for <p> as direct child of <table>")
	}
}

func TestCheckTableContentModel_DivInTable(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <table><div>div directly in table</div></table>
</body>
</html>`

	r := report.NewReport()
	checkTableContentModel([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RSC-005 for <div> as direct child of <table>")
	}
}

func TestCheckTableContentModel_NoFalsePositive(t *testing.T) {
	// Valid table structure: only valid children
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <table>
    <caption>A table</caption>
    <thead><tr><th>Header</th></tr></thead>
    <tbody><tr><td>Cell</td></tr></tbody>
    <tfoot><tr><td>Footer</td></tr></tfoot>
  </table>
</body>
</html>`

	r := report.NewReport()
	checkTableContentModel([]byte(xhtml), "test.xhtml", r)

	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			t.Errorf("unexpected RSC-005 for valid table structure: %s", m.Message)
		}
	}
}

// --- DL/Hgroup Restricted Children Tests ---

func TestCheckRestrictedChildren_SpanInDl(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <dl><span>span inside dl</span></dl>
</body>
</html>`

	r := report.NewReport()
	checkRestrictedChildren([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RSC-005 for <span> inside <dl>")
	}
}

func TestCheckRestrictedChildren_DlValidChildren(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <dl><dt>Term</dt><dd>Definition</dd></dl>
  <dl><div><dt>Term</dt><dd>Definition</dd></div></dl>
</body>
</html>`

	r := report.NewReport()
	checkRestrictedChildren([]byte(xhtml), "test.xhtml", r)

	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			t.Errorf("unexpected RSC-005 for valid dl structure: %s", m.Message)
		}
	}
}

func TestCheckRestrictedChildren_DivInHgroup(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <hgroup><div>div inside hgroup</div></hgroup>
</body>
</html>`

	r := report.NewReport()
	checkRestrictedChildren([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RSC-005 for <div> inside <hgroup>")
	}
}

func TestCheckRestrictedChildren_HgroupValidChildren(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <hgroup><h1>Title</h1><p>Subtitle</p></hgroup>
</body>
</html>`

	r := report.NewReport()
	checkRestrictedChildren([]byte(xhtml), "test.xhtml", r)

	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			t.Errorf("unexpected RSC-005 for valid hgroup: %s", m.Message)
		}
	}
}

// --- Interactive Nesting Tests ---

func TestCheckInteractiveNesting_ButtonInA(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <a href="#"><button>click me</button></a>
</body>
</html>`

	r := report.NewReport()
	checkInteractiveNesting([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RSC-005 for <button> inside <a>")
	}
}

func TestCheckInteractiveNesting_InputInButton(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <button><input type="text"/></button>
</body>
</html>`

	r := report.NewReport()
	checkInteractiveNesting([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RSC-005 for <input> inside <button>")
	}
}

func TestCheckInteractiveNesting_NoFalsePositive(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <a href="#">link</a>
  <button>button</button>
  <div><a href="#">link in div</a><button>button in div</button></div>
</body>
</html>`

	r := report.NewReport()
	checkInteractiveNesting([]byte(xhtml), "test.xhtml", r)

	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			t.Errorf("unexpected RSC-005 for non-nested interactive: %s", m.Message)
		}
	}
}

// --- Transparent Content Model Tests ---

func TestCheckTransparentContentModel_DivInAInP(t *testing.T) {
	// <p><a><div>...</div></a></p> — <a> inherits phrasing from <p>
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <p><a href="#"><div>block in transparent a in p</div></a></p>
</body>
</html>`

	r := report.NewReport()
	checkTransparentContentModel([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RSC-005 for <div> inside <a> inside <p> (transparent inheritance)")
	}
}

func TestCheckTransparentContentModel_DivInInsInSpan(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <p><ins><div>block in ins in p</div></ins></p>
</body>
</html>`

	r := report.NewReport()
	checkTransparentContentModel([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RSC-005 for <div> inside <ins> inside <p> (transparent inheritance)")
	}
}

func TestCheckTransparentContentModel_NoFalsePositive(t *testing.T) {
	// <div><a><div>...</div></a></div> — <a> inherits flow from <div>, so block is OK
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <div><a href="#"><div>block in a in div is OK</div></a></div>
  <section><ins><p>paragraph in ins in section is OK</p></ins></section>
</body>
</html>`

	r := report.NewReport()
	checkTransparentContentModel([]byte(xhtml), "test.xhtml", r)

	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			t.Errorf("unexpected RSC-005: %s", m.Message)
		}
	}
}

// --- Figcaption Position Tests ---

func TestCheckFigcaptionPosition_MiddleFigcaption(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <figure>
    <p>Before</p>
    <figcaption>Caption in middle</figcaption>
    <p>After</p>
  </figure>
</body>
</html>`

	r := report.NewReport()
	checkFigcaptionPosition([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RSC-005 for <figcaption> in middle of <figure>")
	}
}

func TestCheckFigcaptionPosition_FirstOrLast(t *testing.T) {
	// Valid positions: first child or last child
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <figure>
    <figcaption>Caption first</figcaption>
    <p>Content</p>
  </figure>
  <figure>
    <p>Content</p>
    <figcaption>Caption last</figcaption>
  </figure>
</body>
</html>`

	r := report.NewReport()
	checkFigcaptionPosition([]byte(xhtml), "test.xhtml", r)

	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			t.Errorf("unexpected RSC-005 for valid figcaption position: %s", m.Message)
		}
	}
}

// --- Picture Content Model Tests ---

func TestCheckPictureContentModel_InvalidChild(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <picture>
    <div>invalid child</div>
    <img src="test.png" alt="test"/>
  </picture>
</body>
</html>`

	r := report.NewReport()
	checkPictureContentModel([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RSC-005 for <div> inside <picture>")
	}
}

func TestCheckPictureContentModel_SourceAfterImg(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <picture>
    <img src="test.png" alt="test"/>
    <source srcset="test2.png"/>
  </picture>
</body>
</html>`

	r := report.NewReport()
	checkPictureContentModel([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RSC-005 for <source> after <img> in <picture>")
	}
}

func TestCheckPictureContentModel_ValidStructure(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <picture>
    <source srcset="large.png" media="(min-width: 800px)"/>
    <source srcset="small.png"/>
    <img src="fallback.png" alt="test"/>
  </picture>
</body>
</html>`

	r := report.NewReport()
	checkPictureContentModel([]byte(xhtml), "test.xhtml", r)

	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			t.Errorf("unexpected RSC-005 for valid picture structure: %s", m.Message)
		}
	}
}

func TestCheckEpubTypeValid_InvalidEpubType(t *testing.T) {
	// A proper epub:type attribute with an invalid value SHOULD trigger HTM-015
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops">
<head><title>Test</title></head>
<body>
  <section epub:type="madeupvalue"><p>Hello</p></section>
</body>
</html>`

	r := report.NewReport()
	checkEpubTypeValid([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "HTM-015" {
			found = true
			break
		}
	}
	if !found {
		t.Error("invalid epub:type value should trigger HTM-015")
	}
}

// --- Disallowed Descendants Tests ---

func TestCheckDisallowedDescendants_AddressInAddress(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <address>
    <address>nested address</address>
  </address>
</body>
</html>`

	r := report.NewReport()
	checkDisallowedDescendants([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RSC-005 for <address> inside <address>")
	}
}

func TestCheckDisallowedDescendants_HeaderInAddress(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <address>
    <header>header in address</header>
  </address>
</body>
</html>`

	r := report.NewReport()
	checkDisallowedDescendants([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RSC-005 for <header> inside <address>")
	}
}

func TestCheckDisallowedDescendants_FormInForm(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <form>
    <form>nested form</form>
  </form>
</body>
</html>`

	r := report.NewReport()
	checkDisallowedDescendants([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RSC-005 for <form> inside <form>")
	}
}

func TestCheckDisallowedDescendants_TableInCaption(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <table>
    <caption>
      <table><tr><td>nested table in caption</td></tr></table>
    </caption>
  </table>
</body>
</html>`

	r := report.NewReport()
	checkDisallowedDescendants([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RSC-005 for <table> inside <caption>")
	}
}

func TestCheckDisallowedDescendants_FooterInHeader(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <header>
    <footer>footer in header</footer>
  </header>
</body>
</html>`

	r := report.NewReport()
	checkDisallowedDescendants([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RSC-005 for <footer> inside <header>")
	}
}

func TestCheckDisallowedDescendants_ValidNesting(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <address><p>valid content</p></address>
  <header><p>valid header</p></header>
  <footer><p>valid footer</p></footer>
  <form><input type="text"/></form>
</body>
</html>`

	r := report.NewReport()
	checkDisallowedDescendants([]byte(xhtml), "test.xhtml", r)

	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			t.Errorf("unexpected RSC-005 for valid nesting: %s", m.Message)
		}
	}
}

func TestCheckDisallowedDescendants_LabelInLabel(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <label>outer <label>inner label</label></label>
</body>
</html>`

	r := report.NewReport()
	checkDisallowedDescendants([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RSC-005 for <label> inside <label>")
	}
}

func TestCheckDisallowedDescendants_ProgressInProgress(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <progress><progress>nested</progress></progress>
</body>
</html>`

	r := report.NewReport()
	checkDisallowedDescendants([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RSC-005 for <progress> inside <progress>")
	}
}

func TestCheckDisallowedDescendants_MeterInMeter(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <meter value="1"><meter value="2">nested</meter></meter>
</body>
</html>`

	r := report.NewReport()
	checkDisallowedDescendants([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RSC-005 for <meter> inside <meter>")
	}
}

// --- Required Ancestor Tests ---

func TestCheckRequiredAncestor_AreaWithoutMap(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <area shape="default" href="#"/>
</body>
</html>`

	r := report.NewReport()
	checkRequiredAncestor([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RSC-005 for <area> without <map> ancestor")
	}
}

func TestCheckRequiredAncestor_AreaInsideMap(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <map name="test"><area shape="default" href="#"/></map>
</body>
</html>`

	r := report.NewReport()
	checkRequiredAncestor([]byte(xhtml), "test.xhtml", r)

	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			t.Errorf("unexpected RSC-005 for <area> inside <map>: %s", m.Message)
		}
	}
}

func TestCheckRequiredAncestor_ImgIsmapWithoutAHref(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <img src="map.png" ismap="ismap" alt="map"/>
</body>
</html>`

	r := report.NewReport()
	checkRequiredAncestor([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RSC-005 for <img ismap> without <a href> ancestor")
	}
}

func TestCheckRequiredAncestor_ImgIsmapInsideAHref(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <a href="map-handler"><img src="map.png" ismap="ismap" alt="map"/></a>
</body>
</html>`

	r := report.NewReport()
	checkRequiredAncestor([]byte(xhtml), "test.xhtml", r)

	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			t.Errorf("unexpected RSC-005 for <img ismap> inside <a href>: %s", m.Message)
		}
	}
}

// --- BDO Dir Test ---

func TestCheckBdoDir_MissingDir(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <p><bdo>missing dir attribute</bdo></p>
</body>
</html>`

	r := report.NewReport()
	checkBdoDir([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RSC-005 for <bdo> without dir attribute")
	}
}

func TestCheckBdoDir_WithDir(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <p><bdo dir="rtl">right to left text</bdo></p>
</body>
</html>`

	r := report.NewReport()
	checkBdoDir([]byte(xhtml), "test.xhtml", r)

	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			t.Errorf("unexpected RSC-005 for <bdo dir='rtl'>: %s", m.Message)
		}
	}
}

// --- SSML Ph Nesting Tests ---

func TestCheckSSMLPhNesting_Nested(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:ssml="http://www.w3.org/2001/10/synthesis">
<head><title>Test</title></head>
<body>
  <p ssml:ph="outer">
    <span ssml:ph="inner">Nested ssml:ph is not allowed</span>
  </p>
</body>
</html>`

	r := report.NewReport()
	checkSSMLPhNesting([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RSC-005 for nested ssml:ph attributes")
	}
}

func TestCheckSSMLPhNesting_Siblings(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:ssml="http://www.w3.org/2001/10/synthesis">
<head><title>Test</title></head>
<body>
  <p ssml:alphabet="ipa">
    <span ssml:ph="first">word1</span>
    <span ssml:ph="second">word2</span>
  </p>
</body>
</html>`

	r := report.NewReport()
	checkSSMLPhNesting([]byte(xhtml), "test.xhtml", r)

	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			t.Errorf("unexpected RSC-005 for sibling ssml:ph attributes: %s", m.Message)
		}
	}
}

// --- Duplicate Map Name Tests ---

func TestCheckDuplicateMapName_Duplicate(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <map name="dup"><area shape="default" href="#a" alt="a"/></map>
  <map name="dup"><area shape="default" href="#b" alt="b"/></map>
</body>
</html>`

	r := report.NewReport()
	checkDuplicateMapName([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RSC-005 for duplicate map names")
	}
}

func TestCheckDuplicateMapName_Unique(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <map name="map1"><area shape="default" href="#a" alt="a"/></map>
  <map name="map2"><area shape="default" href="#b" alt="b"/></map>
</body>
</html>`

	r := report.NewReport()
	checkDuplicateMapName([]byte(xhtml), "test.xhtml", r)

	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			t.Errorf("unexpected RSC-005 for unique map names: %s", m.Message)
		}
	}
}

// --- Select Multiple Tests ---

func TestCheckSelectMultiple_MultipleSelectedWithoutMultiple(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <select>
    <option selected="selected">A</option>
    <option selected="selected">B</option>
  </select>
</body>
</html>`

	r := report.NewReport()
	checkSelectMultiple([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RSC-005 for multiple selected without @multiple")
	}
}

func TestCheckSelectMultiple_MultipleSelectedWithMultiple(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
  <select multiple="multiple">
    <option selected="selected">A</option>
    <option selected="selected">B</option>
  </select>
</body>
</html>`

	r := report.NewReport()
	checkSelectMultiple([]byte(xhtml), "test.xhtml", r)

	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			t.Errorf("unexpected RSC-005 with @multiple attribute: %s", m.Message)
		}
	}
}

// --- Meta Charset Tests ---

func TestCheckMetaCharset_Duplicate(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head>
  <meta charset="utf-8"/>
  <meta charset="utf-8"/>
  <title>Test</title>
</head>
<body><p>test</p></body>
</html>`

	r := report.NewReport()
	checkMetaCharset([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RSC-005 for duplicate meta charset")
	}
}

func TestCheckMetaCharset_Single(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head>
  <meta charset="utf-8"/>
  <title>Test</title>
</head>
<body><p>test</p></body>
</html>`

	r := report.NewReport()
	checkMetaCharset([]byte(xhtml), "test.xhtml", r)

	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			t.Errorf("unexpected RSC-005 for single meta charset: %s", m.Message)
		}
	}
}

// --- Link Sizes Tests ---

func TestCheckLinkSizes_SizesWithoutRelIcon(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head>
  <title>Test</title>
  <link rel="stylesheet" sizes="16x16" href="style.css"/>
</head>
<body><p>test</p></body>
</html>`

	r := report.NewReport()
	checkLinkSizes([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RSC-005 for link sizes without rel=icon")
	}
}

func TestCheckLinkSizes_SizesWithRelIcon(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head>
  <title>Test</title>
  <link rel="icon" sizes="16x16" href="icon.png"/>
</head>
<body><p>test</p></body>
</html>`

	r := report.NewReport()
	checkLinkSizes([]byte(xhtml), "test.xhtml", r)

	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			t.Errorf("unexpected RSC-005 for link sizes with rel=icon: %s", m.Message)
		}
	}
}

// --- Tests for checkInvalidIDValues (colon-containing IDs) ---

func TestCheckInvalidIDValues_ColonInID(t *testing.T) {
	// Calibre generates footnote IDs like fn:1 and fnref:2 which contain colons.
	// These are not valid XML NCNames.
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
<p>Text<sup id="fnref:1">1</sup></p>
<div id="fn:1"><p>Footnote</p></div>
</body>
</html>`

	r := report.NewReport()
	checkInvalidIDValues([]byte(xhtml), "test.xhtml", r)

	found := 0
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" && strings.Contains(m.Message, "without colons") {
			found++
		}
	}
	if found != 2 {
		t.Errorf("expected 2 RSC-005 errors for colon IDs, got %d", found)
	}
}

func TestCheckInvalidIDValues_ValidIDs(t *testing.T) {
	// Standard IDs without colons should not trigger errors.
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
<p id="para1">Text</p>
<div id="chapter-one"><p id="_note">Note</p></div>
</body>
</html>`

	r := report.NewReport()
	checkInvalidIDValues([]byte(xhtml), "test.xhtml", r)

	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" && strings.Contains(m.Message, "without colons") {
			t.Errorf("unexpected RSC-005 for valid ID: %s", m.Message)
		}
	}
}

func TestCheckInvalidIDValues_MultipleColons(t *testing.T) {
	// IDs with multiple colons like xml:id:value
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body><p id="ns:sub:value">Text</p></body>
</html>`

	r := report.NewReport()
	checkInvalidIDValues([]byte(xhtml), "test.xhtml", r)

	found := 0
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" && strings.Contains(m.Message, "without colons") {
			found++
		}
	}
	if found != 1 {
		t.Errorf("expected 1 RSC-005 for colon ID, got %d", found)
	}
}

// --- Tests for checkHTML5ElementsEPUB2 ---

func TestCheckHTML5ElementsEPUB2_ArticleElement(t *testing.T) {
	// HTML5 <article> element is not valid in EPUB 2 XHTML 1.1
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
<article><p>Content</p></article>
</body>
</html>`

	r := report.NewReport()
	checkHTML5ElementsEPUB2([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" && strings.Contains(m.Message, "article") {
			found = true
		}
	}
	if !found {
		t.Error("expected RSC-005 for <article> in EPUB 2, got none")
	}
}

func TestCheckHTML5ElementsEPUB2_FooterElement(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
<footer><p>Footer content</p></footer>
</body>
</html>`

	r := report.NewReport()
	checkHTML5ElementsEPUB2([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" && strings.Contains(m.Message, "footer") {
			found = true
		}
	}
	if !found {
		t.Error("expected RSC-005 for <footer> in EPUB 2, got none")
	}
}

func TestCheckHTML5ElementsEPUB2_MarkElement(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
<p>Some <mark>highlighted</mark> text.</p>
</body>
</html>`

	r := report.NewReport()
	checkHTML5ElementsEPUB2([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" && strings.Contains(m.Message, "mark") {
			found = true
		}
	}
	if !found {
		t.Error("expected RSC-005 for <mark> in EPUB 2, got none")
	}
}

func TestCheckHTML5ElementsEPUB2_TimeElement(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
<p>Published on <time datetime="2024-01-15">January 15</time>.</p>
</body>
</html>`

	r := report.NewReport()
	checkHTML5ElementsEPUB2([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" && strings.Contains(m.Message, "time") {
			found = true
		}
	}
	if !found {
		t.Error("expected RSC-005 for <time> in EPUB 2, got none")
	}
}

func TestCheckHTML5ElementsEPUB2_NoHTML5Elements(t *testing.T) {
	// Valid EPUB 2 XHTML with only standard elements — no errors expected
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
<h1>Chapter</h1>
<p>A <strong>paragraph</strong> with <em>formatting</em>.</p>
<div><p>A div with content.</p></div>
<table><tr><td>Cell</td></tr></table>
</body>
</html>`

	r := report.NewReport()
	checkHTML5ElementsEPUB2([]byte(xhtml), "test.xhtml", r)

	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" {
			t.Errorf("unexpected RSC-005 for valid EPUB 2 XHTML: %s", m.Message)
		}
	}
}

func TestCheckHTML5ElementsEPUB2_AsideElement(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
<aside><p>Sidebar content.</p></aside>
</body>
</html>`

	r := report.NewReport()
	checkHTML5ElementsEPUB2([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" && strings.Contains(m.Message, "aside") {
			found = true
		}
	}
	if !found {
		t.Error("expected RSC-005 for <aside> in EPUB 2, got none")
	}
}

func TestCheckHTML5ElementsEPUB2_HeaderElement(t *testing.T) {
	xhtml := `<?xml version="1.0" encoding="UTF-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Test</title></head>
<body>
<header><h1>Title</h1></header>
</body>
</html>`

	r := report.NewReport()
	checkHTML5ElementsEPUB2([]byte(xhtml), "test.xhtml", r)

	found := false
	for _, m := range r.Messages {
		if m.CheckID == "RSC-005" && strings.Contains(m.Message, "header") {
			found = true
		}
	}
	if !found {
		t.Error("expected RSC-005 for <header> in EPUB 2, got none")
	}
}
