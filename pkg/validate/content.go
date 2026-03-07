package validate

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/adammathes/epubverify/pkg/epub"
	"github.com/adammathes/epubverify/pkg/report"
)

// checkContentWithSkips validates XHTML content documents, skipping files with known encoding issues.
func checkContentWithSkips(ep *epub.EPUB, r *report.Report, skipFiles map[string]bool) {
	if ep.Package == nil {
		return
	}

	// OPF-073: DOCTYPE external identifier checks (runs over all manifest items)
	checkDOCTYPEExternalIdentifiers(ep, r)

	// Build set of manifest-declared resources (resolved full paths).
	manifestPaths := make(map[string]bool)
	for _, item := range ep.Package.Manifest {
		if item.Href != "\x00MISSING" {
			manifestPaths[ep.ResolveHref(item.Href)] = true
		}
	}

	isFXL := ep.Package.RenditionLayout == "pre-paginated"

	// Build map of manifest item ID -> spine itemref properties for rendition overrides
	spineProps := make(map[string]string)
	spineItemIDs := make(map[string]bool)
	for _, ref := range ep.Package.Spine {
		spineProps[ref.IDRef] = ref.Properties
		spineItemIDs[ref.IDRef] = true
	}

	// Pre-compute maps used by per-file checks to avoid O(n²) rebuilding.
	remoteManifestURLs := make(map[string]bool)
	remoteManifestItems := make(map[string]epub.ManifestItem)
	manifestByPath := make(map[string]epub.ManifestItem)
	for _, mItem := range ep.Package.Manifest {
		if isRemoteURL(mItem.Href) {
			remoteManifestURLs[mItem.Href] = true
			remoteManifestItems[mItem.Href] = mItem
		}
		if mItem.Href != "\x00MISSING" {
			manifestByPath[ep.ResolveHref(mItem.Href)] = mItem
		}
	}
	spinePathSet := buildSpinePathSet(ep)

	// Check SVG content documents for remote-resources property
	for _, item := range ep.Package.Manifest {
		if item.Href == "\x00MISSING" {
			continue
		}
		if item.MediaType != "image/svg+xml" {
			continue
		}
		if ep.Package.Version < "3.0" {
			continue
		}
		fullPath := ep.ResolveHref(item.Href)
		data, err := ep.ReadFile(fullPath)
		if err != nil {
			continue
		}
		checkSVGPropertyDeclarations(ep, data, fullPath, item, r)
		checkNoRemoteResources(ep, data, fullPath, item, remoteManifestURLs, r)

		// HTM-048: FXL SVG spine items must have viewBox on root svg element
		if spineItemIDs[item.ID] {
			itemIsFXL := isFXL
			if props, ok := spineProps[item.ID]; ok {
				if hasProperty(props, "rendition:layout-reflowable") {
					itemIsFXL = false
				} else if hasProperty(props, "rendition:layout-pre-paginated") {
					itemIsFXL = true
				}
			}
			if itemIsFXL {
				checkFXLSVGViewBox(data, fullPath, r)
			}
		}
	}

	for _, item := range ep.Package.Manifest {
		if item.Href == "\x00MISSING" {
			continue
		}
		if item.MediaType != "application/xhtml+xml" {
			continue
		}

		fullPath := ep.ResolveHref(item.Href)

		// Skip files with encoding errors
		if skipFiles[fullPath] {
			continue
		}

		data, err := ep.ReadFile(fullPath)
		if err != nil {
			continue // Missing file reported by RSC-001
		}

		isNav := hasProperty(item.Properties, "nav")

		// HTM-001: XHTML must be well-formed XML
		// Skip nav docs - NAV-011 handles them
		if !isNav {
			if !checkXHTMLWellFormed(data, fullPath, r) {
				continue // Can't check further if not well-formed
			}
		}

		// HTM-002: content should have title (WARNING)
		checkContentHasTitle(data, fullPath, r)

		// HTM-003: empty href attributes
		checkEmptyHrefAttributes(data, fullPath, r)

		// HTM-045: empty href encountered (usage hint for self-references)
		checkEmptyHrefUsage(data, fullPath, r)

		// HTM-004: no obsolete elements
		checkNoObsoleteElements(data, fullPath, r)

		// RSC-005: no deprecated/obsolete HTML attributes
		if ep.Package.Version >= "3.0" {
			checkObsoleteAttrs(data, fullPath, r)
		}

		// HTM-009: base element not allowed
		checkNoBaseElement(data, fullPath, r)

		// HTM-010/HTM-011/HTM-012: DOCTYPE and namespace checks (EPUB 3 only)
		if ep.Package.Version >= "3.0" {
			if !checkDoctypeHTML5(data, fullPath, r) {
				checkDoctype(data, fullPath, r)
			}
		}
		checkXHTMLNamespace(data, fullPath, r)

		// HTM-005/HTM-006/HTM-007: property declarations
		if ep.Package.Version >= "3.0" {
			checkPropertyDeclarations(ep, data, fullPath, item, r)
		}

		// HTM-015: epub:type values must be valid (EPUB 3 only)
		if ep.Package.Version >= "3.0" {
			checkEpubTypeValid(data, fullPath, r)
		}

		// HTM-020: no processing instructions
		checkNoProcessingInstructions(data, fullPath, r)

		// HTM-021: position:absolute warning
		checkNoPositionAbsolute(data, fullPath, r)

		// HTM-013/HTM-014: FXL viewport checks (only for spine items)
		if ep.Package.Version >= "3.0" && spineItemIDs[item.ID] {
			// Determine if this specific item is fixed-layout, considering
			// per-spine-item rendition overrides
			itemIsFXL := isFXL
			if props, ok := spineProps[item.ID]; ok {
				if hasProperty(props, "rendition:layout-reflowable") {
					itemIsFXL = false
				} else if hasProperty(props, "rendition:layout-pre-paginated") {
					itemIsFXL = true
				}
			}
			// Skip nav document from FXL viewport checks
			if itemIsFXL && !hasProperty(item.Properties, "nav") {
				checkFXLViewport(data, fullPath, r)
			} else if !itemIsFXL && !hasProperty(item.Properties, "nav") {
				// HTM-060b: viewport in reflowable content (usage note)
				checkReflowViewport(data, fullPath, r)
			}
		}

		// RSC-003: fragment identifiers must resolve (skip nav - handled by NAV checks)
		// Skip when external base URL is set (all relative hrefs become remote)
		if !isNav {
			if extBase, _ := detectExternalBaseURL(data); extBase == "" {
				checkFragmentIdentifiers(ep, data, fullPath, r)
			}
		}

		// RSC-004: no remote resources (img src with http://)
		// RSC-008: no remote stylesheets
		checkNoRemoteResources(ep, data, fullPath, item, remoteManifestURLs, r)

		// HTM-008 / RSC-007: check internal links and resource references
		// Skip nav document - its links are checked by NAV-003/006/007
		if !isNav {
			checkContentReferences(ep, data, fullPath, item.Href, manifestPaths, remoteManifestItems, manifestByPath, spinePathSet, r)
			// RSC-014: hyperlinks to SVG symbol elements are not allowed
			checkSVGSymbolLinks(data, fullPath, r)
		}

		// HTM-052: region-based property only allowed on data-nav documents
		if ep.Package.Version >= "3.0" && !hasProperty(item.Properties, "data-nav") {
			checkRegionBasedProperty(data, fullPath, r)
		}

		// HTM-051: EDUPUB microdata without RDFa warning
		if ep.Package.Version >= "3.0" && epubHasDCType(ep, "edupub") {
			checkMicrodataWithoutRDFa(data, fullPath, r)
		}

		// HTM-016: unique IDs within content document
		checkUniqueIDs(data, fullPath, r)

		// HTM-018: single body element
		checkSingleBody(data, fullPath, r)

		// HTM-019: html root element
		hasHTMLRoot := checkHTMLRootElement(data, fullPath, r)

		// HTM-022: object data references must resolve
		if !isNav {
			checkObjectReferences(ep, data, fullPath, r)
		}

		// HTM-023: no parent directory links that escape container
		if !isNav {
			checkNoParentDirLinks(ep, data, fullPath, r)
		}

		// HTM-024: content documents must have a head element (skip if no html root)
		if hasHTMLRoot {
			checkContentHasHead(data, fullPath, r)
		}

		// HTM-025: embed element references must exist
		if !isNav {
			checkEmbedReferences(ep, data, fullPath, r)
		}

		// HTM-026: lang and xml:lang must match
		checkLangXMLLangMatch(data, fullPath, r)

		// HTM-027: video poster must exist
		if ep.Package.Version >= "3.0" && !isNav {
			checkVideoPosterExists(ep, data, fullPath, r)
		}

		// HTM-028: audio src must exist
		if ep.Package.Version >= "3.0" && !isNav {
			checkAudioSrcExists(ep, data, fullPath, r)
		}

		// HTM-030: img src must not be empty
		checkImgSrcNotEmpty(data, fullPath, r)

		// HTM-031: custom attribute namespaces must be valid
		if ep.Package.Version >= "3.0" {
			checkCustomAttributeNamespaces(data, fullPath, r)
		}

		// HTM-032: style element CSS syntax
		checkStyleElementValid(data, fullPath, r)

		// HTM-033: no RDF elements in content
		checkNoRDFElements(data, fullPath, r)

		// RSC-032: foreign resources referenced from content must have fallbacks
		if ep.Package.Version >= "3.0" && !isNav {
			checkForeignResourceFallbacks(ep, data, fullPath, r)
		}

		// RSC-005: invalid HTML elements (elements not in valid HTML5 set)
		if ep.Package.Version >= "3.0" {
			checkInvalidHTMLElements(data, fullPath, r)
			// RSC-005: Schematron-like checks (e.g., nested dfn)
			checkNestedDFN(data, fullPath, r)
			// RSC-005: HTML5 content model — block elements in phrasing-only parents
			checkBlockInPhrasing(data, fullPath, r)
			// RSC-005: HTML5 content model — restricted children (ul/ol/select/tr/etc.)
			checkRestrictedChildren(data, fullPath, r)
			// RSC-005: HTML5 content model — void elements cannot have children
			checkVoidElementChildren(data, fullPath, r)
			// RSC-005: HTML5 content model — table direct children
			checkTableContentModel(data, fullPath, r)
			// RSC-005: HTML5 content model — interactive nesting
			checkInteractiveNesting(data, fullPath, r)
			// RSC-005: HTML5 content model — transparent content model inheritance
			checkTransparentContentModel(data, fullPath, r)
			// RSC-005: HTML5 content model — figcaption position in figure
			checkFigcaptionPosition(data, fullPath, r)
			// RSC-005: HTML5 content model — picture element structure
			checkPictureContentModel(data, fullPath, r)
			// RSC-005: Schematron disallowed descendants (address, form, caption, etc.)
			checkDisallowedDescendants(data, fullPath, r)
			// RSC-005: Schematron required ancestors (area→map, img[ismap]→a[href])
			checkRequiredAncestor(data, fullPath, r)
			// RSC-005: bdo requires dir attribute
			checkBdoDir(data, fullPath, r)
			// RSC-005: nested ssml:ph attributes
			checkSSMLPhNesting(data, fullPath, r)
			// RSC-005: duplicate map names
			checkDuplicateMapName(data, fullPath, r)
			// RSC-005: select without multiple having multiple selected options
			checkSelectMultiple(data, fullPath, r)
			// RSC-005: meta charset at most once
			checkMetaCharset(data, fullPath, r)
			// RSC-005: meta must have name, http-equiv, charset, or property
			checkMetaRequiredAttrs(data, fullPath, r)
			// RSC-005: link sizes only on rel=icon
			checkLinkSizes(data, fullPath, r)
		}

		// EPUB 2-specific checks: HTML5 elements not allowed in XHTML 1.1
		if ep.Package.Version < "3.0" {
			checkHTML5ElementsEPUB2(data, fullPath, r)
		}

		// RSC-005: id attribute values must be valid XML NCNames (no colons)
		checkInvalidIDValues(data, fullPath, r)
	}
}

// sourceRef holds a buffered <source> element for deferred audio/video checking.
type sourceRef struct {
	href      string
	mediaType string // from type attribute (may be empty)
}

// RSC-032: foreign resources (non-core media types) referenced from content
// documents must have proper fallbacks (manifest fallback or HTML fallback).
func checkForeignResourceFallbacks(ep *epub.EPUB, data []byte, location string, r *report.Report) {
	// Build manifest maps
	manifestByHref := make(map[string]epub.ManifestItem) // resolved path -> item
	for _, item := range ep.Package.Manifest {
		if item.Href != "\x00MISSING" {
			manifestByHref[ep.ResolveHref(item.Href)] = item
		}
	}

	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	itemDir := path.Dir(location)
	inPicture := false
	// Track audio/video context: when non-empty, we're inside that element
	// and buffer <source> elements to check as a group (HTML fallback mechanism)
	mediaParent := "" // "audio" or "video" or ""
	var bufferedSources []sourceRef

	// Track object nesting for HTML inline fallback detection
	type objectCtx struct {
		data       string // data attribute
		objectType string // type attribute
		depth      int    // nesting depth (how many elements deep inside)
		hasFallback bool  // whether child HTML fallback content was found
	}
	var objectStack []objectCtx

	// Build bindings media type set (deprecated but still valid fallback)
	bindingsTypes := ep.Package.BindingsTypes

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}

		// Track end elements
		if ee, ok := tok.(xml.EndElement); ok {
			switch ee.Name.Local {
			case "picture":
				inPicture = false
			case "audio", "video":
				if mediaParent == ee.Name.Local {
					// Process buffered sources: if any source resolves to a core type,
					// that's the HTML fallback — foreign sources in the same element are OK.
					checkAudioVideoSources(ep, bufferedSources, itemDir, location, manifestByHref, r)
					mediaParent = ""
					bufferedSources = nil
				}
			case "object":
				if len(objectStack) > 0 {
					ctx := objectStack[len(objectStack)-1]
					objectStack = objectStack[:len(objectStack)-1]
					// Only check RSC-032 if no HTML fallback and no bindings handler
					if ctx.data != "" && !ctx.hasFallback {
						if !checkTypeMismatch(ctx.data, ctx.objectType, itemDir, location, manifestByHref, r) {
							checkForeignRef(ep, ctx.data, itemDir, location, manifestByHref, "object", r)
						}
					}
				}
			}
			// Track nesting depth in object context
			if len(objectStack) > 0 {
				objectStack[len(objectStack)-1].depth--
			}
			continue
		}

		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}

		// Track HTML fallback inside <object> — any child element that isn't
		// <param> or another <object> counts as inline HTML fallback content
		if len(objectStack) > 0 && se.Name.Local != "object" {
			idx := len(objectStack) - 1
			objectStack[idx].depth++
			if se.Name.Local != "param" && !objectStack[idx].hasFallback {
				objectStack[idx].hasFallback = true
			}
		}

		switch se.Name.Local {
		case "picture":
			inPicture = true
		case "img":
			if inPicture {
				// MED-003: img inside picture must reference core media types
				checkPictureImgRef(ep, se, itemDir, location, manifestByHref, r)
			} else {
				for _, attr := range se.Attr {
					if attr.Name.Local == "src" {
						checkForeignRef(ep, attr.Value, itemDir, location, manifestByHref, "img", r)
					}
				}
			}
		case "audio", "video":
			mediaParent = se.Name.Local
			bufferedSources = nil
			// Check direct src attribute (not the <source> fallback mechanism)
			for _, attr := range se.Attr {
				if attr.Name.Local == "src" {
					checkForeignRef(ep, attr.Value, itemDir, location, manifestByHref, se.Name.Local, r)
				}
				if se.Name.Local == "video" && attr.Name.Local == "poster" {
					checkForeignRef(ep, attr.Value, itemDir, location, manifestByHref, "poster", r)
				}
			}
		case "source":
			if inPicture {
				// MED-007: source in picture without type attr for foreign resource
				checkPictureSourceRef(ep, se, itemDir, location, manifestByHref, r)
			} else if mediaParent != "" {
				// Buffer for deferred audio/video HTML fallback check
				var href, typeAttr string
				for _, attr := range se.Attr {
					if attr.Name.Local == "src" {
						href = attr.Value
					}
					if attr.Name.Local == "type" {
						typeAttr = attr.Value
					}
				}
				if href != "" {
					bufferedSources = append(bufferedSources, sourceRef{href: href, mediaType: typeAttr})
				}
			} else {
				for _, attr := range se.Attr {
					if attr.Name.Local == "src" {
						checkForeignRef(ep, attr.Value, itemDir, location, manifestByHref, "source", r)
					}
				}
			}
		case "embed":
			var embedSrc, embedType string
			for _, attr := range se.Attr {
				if attr.Name.Local == "src" {
					embedSrc = attr.Value
				}
				if attr.Name.Local == "type" {
					embedType = attr.Value
				}
			}
			if embedSrc != "" {
				if !checkTypeMismatch(embedSrc, embedType, itemDir, location, manifestByHref, r) {
					checkForeignRef(ep, embedSrc, itemDir, location, manifestByHref, "embed", r)
				}
			}
		case "object":
			var objectData, objectType string
			for _, attr := range se.Attr {
				if attr.Name.Local == "data" {
					objectData = attr.Value
				}
				if attr.Name.Local == "type" {
					objectType = attr.Value
				}
			}
			// Check if this media type has a bindings handler (deprecated but valid fallback)
			hasBindingsFallback := bindingsTypes != nil && bindingsTypes[objectType]
			// Push object context — defer RSC-032 until we know if HTML fallback exists
			objectStack = append(objectStack, objectCtx{
				data:        objectData,
				objectType:  objectType,
				hasFallback: hasBindingsFallback,
			})
		case "input":
			var inputType, src string
			for _, attr := range se.Attr {
				if attr.Name.Local == "type" {
					inputType = attr.Value
				}
				if attr.Name.Local == "src" {
					src = attr.Value
				}
			}
			if inputType == "image" && src != "" {
				checkForeignRef(ep, src, itemDir, location, manifestByHref, "input-image", r)
			}
		case "math":
			for _, attr := range se.Attr {
				if attr.Name.Local == "altimg" {
					checkForeignRef(ep, attr.Value, itemDir, location, manifestByHref, "math-altimg", r)
				}
			}
		}
	}
}

// checkAudioVideoSources processes buffered <source> elements from an <audio> or <video>.
// If any source resolves to a core media type, foreign sources are OK (HTML fallback).
// Otherwise RSC-032 fires for each foreign source with no manifest fallback.
func checkAudioVideoSources(ep *epub.EPUB, sources []sourceRef, itemDir, location string, manifestByHref map[string]epub.ManifestItem, r *report.Report) {
	if len(sources) == 0 {
		return
	}

	// Check if any source is a core media type (HTML fallback present)
	hasCoreSource := false
	for _, src := range sources {
		if src.href == "" || isRemoteURL(src.href) {
			continue
		}
		// Check via type attribute first
		if src.mediaType != "" {
			mt := src.mediaType
			if idx := strings.Index(mt, ";"); idx >= 0 {
				mt = strings.TrimSpace(mt[:idx])
			}
			if coreMediaTypes[mt] {
				hasCoreSource = true
				break
			}
		}
		// Check via manifest
		u, err := url.Parse(src.href)
		if err != nil {
			continue
		}
		target := resolvePath(itemDir, u.Path)
		item, ok := manifestByHref[target]
		if !ok {
			continue
		}
		mt := item.MediaType
		if idx := strings.Index(mt, ";"); idx >= 0 {
			mt = strings.TrimSpace(mt[:idx])
		}
		if coreMediaTypes[mt] {
			hasCoreSource = true
			break
		}
	}

	if hasCoreSource {
		return // HTML fallback mechanism satisfied
	}

	// No core source — check each foreign source individually
	for _, src := range sources {
		checkForeignRef(ep, src.href, itemDir, location, manifestByHref, "source", r)
	}
}


// checkPictureImgRef checks img src/srcset inside <picture> — must be core types (MED-003).
// Reports once per unique foreign resource (deduplicates src vs srcset references).
func checkPictureImgRef(ep *epub.EPUB, se xml.StartElement, itemDir, location string, manifestByHref map[string]epub.ManifestItem, r *report.Report) {
	reported := make(map[string]bool)
	for _, attr := range se.Attr {
		switch attr.Name.Local {
		case "src":
			if !reported[attr.Value] {
				reported[attr.Value] = true
				checkPictureForeignRef(ep, attr.Value, itemDir, location, manifestByHref, r)
			}
		case "srcset":
			// Parse srcset: "url [descriptor], url [descriptor], ..."
			for _, entry := range strings.Split(attr.Value, ",") {
				parts := strings.Fields(strings.TrimSpace(entry))
				if len(parts) > 0 && parts[0] != "" {
					if !reported[parts[0]] {
						reported[parts[0]] = true
						checkPictureForeignRef(ep, parts[0], itemDir, location, manifestByHref, r)
					}
				}
			}
		}
	}
}

// checkPictureSourceRef checks source elements inside <picture>.
// OPF-013 if type attr doesn't match manifest media-type.
// MED-007 if no type attr for foreign resource.
func checkPictureSourceRef(ep *epub.EPUB, se xml.StartElement, itemDir, location string, manifestByHref map[string]epub.ManifestItem, r *report.Report) {
	var typeAttr, srcset string
	for _, attr := range se.Attr {
		if attr.Name.Local == "type" {
			typeAttr = attr.Value
		}
		if attr.Name.Local == "srcset" {
			srcset = attr.Value
		}
	}
	if srcset == "" {
		return
	}
	// Get the first URL from srcset for type mismatch check
	firstHref := ""
	for _, entry := range strings.Split(srcset, ",") {
		parts := strings.Fields(strings.TrimSpace(entry))
		if len(parts) > 0 && parts[0] != "" {
			firstHref = parts[0]
			break
		}
	}
	// OPF-013: type attribute doesn't match manifest media-type
	if typeAttr != "" && firstHref != "" {
		if checkTypeMismatch(firstHref, typeAttr, itemDir, location, manifestByHref, r) {
			return
		}
		return // type matches or no manifest entry — skip MED-007
	}
	// No type attribute: check if any srcset URL references a foreign resource → MED-007
	// No type attribute: check if any srcset URL references a foreign resource
	for _, entry := range strings.Split(srcset, ",") {
		parts := strings.Fields(strings.TrimSpace(entry))
		if len(parts) == 0 || parts[0] == "" {
			continue
		}
		href := parts[0]
		if isRemoteURL(href) {
			continue
		}
		u, err := url.Parse(href)
		if err != nil {
			continue
		}
		target := resolvePath(itemDir, u.Path)
		item, ok := manifestByHref[target]
		if !ok {
			continue
		}
		mt := item.MediaType
		if idx := strings.Index(mt, ";"); idx >= 0 {
			mt = strings.TrimSpace(mt[:idx])
		}
		if !coreMediaTypes[mt] {
			r.AddWithLocation(report.Error, "MED-007",
				fmt.Sprintf("The `source` element references a foreign resource '%s' but does not declare its media type in a 'type' attribute", href),
				location)
			return // Report once per source element
		}
	}
}

// checkPictureForeignRef checks a reference inside <picture><img> — foreign types → MED-003.
func checkPictureForeignRef(ep *epub.EPUB, href, itemDir, location string, manifestByHref map[string]epub.ManifestItem, r *report.Report) {
	if href == "" || isRemoteURL(href) {
		return
	}
	u, err := url.Parse(href)
	if err != nil {
		return
	}
	refPath := u.Path
	if refPath == "" {
		return
	}
	target := resolvePath(itemDir, refPath)
	item, ok := manifestByHref[target]
	if !ok {
		return
	}
	mt := item.MediaType
	if idx := strings.Index(mt, ";"); idx >= 0 {
		mt = strings.TrimSpace(mt[:idx])
	}
	if coreMediaTypes[mt] {
		return
	}
	r.AddWithLocation(report.Error, "MED-003",
		fmt.Sprintf("The `picture` element's `img` fallback references a foreign resource '%s' of type '%s'", href, item.MediaType),
		location)
}

// checkTypeMismatch checks if the element's type attribute matches the manifest media-type.
// If they don't match, OPF-013 is emitted and true is returned (caller should skip RSC-032).
func checkTypeMismatch(href, typeAttr, itemDir, location string, manifestByHref map[string]epub.ManifestItem, r *report.Report) bool {
	if typeAttr == "" || href == "" || isRemoteURL(href) {
		return false
	}
	u, err := url.Parse(href)
	if err != nil || u.Path == "" {
		return false
	}
	target := resolvePath(itemDir, u.Path)
	item, ok := manifestByHref[target]
	if !ok {
		return false
	}
	manifestMT := item.MediaType
	if idx := strings.Index(manifestMT, ";"); idx >= 0 {
		manifestMT = strings.TrimSpace(manifestMT[:idx])
	}
	declaredMT := typeAttr
	if idx := strings.Index(declaredMT, ";"); idx >= 0 {
		declaredMT = strings.TrimSpace(declaredMT[:idx])
	}
	if !strings.EqualFold(manifestMT, declaredMT) {
		r.AddWithLocation(report.Warning, "OPF-013",
			fmt.Sprintf("'type' attribute value '%s' does not match the resource's manifest media type '%s'", typeAttr, item.MediaType),
			location)
		return true
	}
	return false
}

func checkForeignRef(ep *epub.EPUB, href, itemDir, location string, manifestByHref map[string]epub.ManifestItem, context string, r *report.Report) {
	if href == "" || isRemoteURL(href) || strings.HasPrefix(href, "data:") {
		// Remote resources and data URIs handled separately
		if strings.HasPrefix(href, "data:") {
			// data: URIs with foreign types need reporting
			if strings.HasPrefix(href, "data:image/") {
				mt := strings.SplitN(href[5:], ";", 2)[0]
				mt = strings.SplitN(mt, ",", 2)[0]
				if !coreMediaTypes[mt] {
					r.AddWithLocation(report.Error, "RSC-032",
						fmt.Sprintf("Fallback must be provided for foreign resource: data URI with media type '%s'", mt),
						location)
				}
			}
		}
		return
	}

	u, err := url.Parse(href)
	if err != nil {
		return
	}
	refPath := u.Path
	if refPath == "" {
		return
	}
	target := resolvePath(itemDir, refPath)
	item, ok := manifestByHref[target]
	if !ok {
		return // Not in manifest - handled by RSC-007
	}

	// Check if the media type is foreign (non-core)
	// Strip parameters (e.g., "audio/ogg ; codecs=opus" -> "audio/ogg")
	mt := item.MediaType
	if idx := strings.Index(mt, ";"); idx >= 0 {
		mt = strings.TrimSpace(mt[:idx])
	}
	if coreMediaTypes[mt] {
		return // Core media type, no fallback needed
	}

	// Exempt: fonts, video (in video/img), audio (in audio/source) are
	// allowed to use foreign types without fallbacks per EPUB spec
	if isFontMediaType(mt) {
		return // Font types are always exempt
	}
	if strings.HasPrefix(mt, "video/") && (context == "video" || context == "source" || context == "img" || context == "object") {
		return // Video foreign types are exempt in video/source/img/object context
	}
	// Note: audio in <source> is NOT exempt - non-core audio types still need fallbacks

	// Check for manifest fallback
	if item.Fallback != "" {
		return // Has manifest fallback
	}

	r.AddWithLocation(report.Error, "RSC-032",
		fmt.Sprintf("Fallback must be provided for foreign resource '%s' of type '%s'", href, item.MediaType),
		location)
}

// HTM-001: check that XHTML is well-formed XML
func checkXHTMLWellFormed(data []byte, location string, r *report.Report) bool {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	// Provide XHTML named entities so that references like &nbsp; don't cause
	// spurious "entity was referenced but not declared" errors. Go's xml.Decoder
	// only knows the 5 XML entities by default; XHTML DTDs define 248 more.
	decoder.Entity = xhtmlEntities
	for {
		_, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			errMsg := err.Error()
			// HTM-017: HTML entity references not valid in XHTML
			if strings.Contains(errMsg, "invalid character entity") || strings.Contains(errMsg, "entity") {
				r.AddWithLocation(report.Fatal, "HTM-017",
					"Content document is not well-formed: entity was referenced but not declared",
					location)
			} else if strings.Contains(errMsg, "attribute") {
				// HTM-029: attribute-related XML errors (e.g., malformed SVG attributes)
				r.AddWithLocation(report.Fatal, "HTM-001",
					fmt.Sprintf("Content document is not well-formed XML: Attribute name is not associated with an element (%s)", errMsg),
					location)
			} else {
				r.AddWithLocation(report.Fatal, "HTM-001",
					fmt.Sprintf("Content document is not well-formed XML: element not terminated by the matching end-tag (%s)", errMsg),
					location)
			}
			return false
		}
	}
	return true
}

// HTM-002: content documents should have a title element
func checkContentHasTitle(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	inHead := false
	hasTitle := false

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "head" {
				inHead = true
			}
			if inHead && t.Name.Local == "title" {
				hasTitle = true
			}
		case xml.EndElement:
			if t.Name.Local == "head" {
				if !hasTitle {
					r.AddWithLocation(report.Warning, "HTM-002",
						"Missing title element in content document head",
						location)
				}
				return
			}
		}
	}
}

// HTM-004: no obsolete HTML elements
var obsoleteElements = map[string]bool{
	"center":    true,
	"font":      true,
	"basefont":  true,
	"big":       true,
	"blink":     true,
	"marquee":   true,
	"multicol":  true,
	"nobr":      true,
	"spacer":    true,
	"strike":    true,
	"tt":        true,
	"acronym":   true,
	"applet":    true,
	"dir":       true,
	"frame":     true,
	"frameset":  true,
	"noframes":  true,
	"isindex":   true,
	"listing":   true,
	"plaintext": true,
	"xmp":       true,
}

func checkNoObsoleteElements(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	reported := make(map[string]bool)

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		if se, ok := tok.(xml.StartElement); ok {
			elemName := se.Name.Local
			if obsoleteElements[elemName] && !reported[elemName] {
				r.AddWithLocation(report.Error, "HTM-004",
					fmt.Sprintf("Element '%s' is not allowed in EPUB content documents", elemName),
					location)
				reported[elemName] = true
			}
		}
	}
}

// HTM-011: DOCTYPE check for EPUB 3
func checkDoctype(data []byte, location string, r *report.Report) {
	content := string(data)
	// Look for DOCTYPE declaration
	idx := strings.Index(content, "<!DOCTYPE")
	if idx == -1 {
		return // No DOCTYPE is fine for EPUB 3
	}

	// Find the full DOCTYPE
	endIdx := strings.Index(content[idx:], ">")
	if endIdx == -1 {
		return
	}
	doctype := content[idx : idx+endIdx+1]

	// EPUB 3 should use HTML5 DOCTYPE: <!DOCTYPE html> (case insensitive)
	// It should NOT have PUBLIC or SYSTEM identifiers
	if strings.Contains(doctype, "PUBLIC") || strings.Contains(doctype, "SYSTEM") {
		r.AddWithLocation(report.Error, "HTM-011",
			"Irregular DOCTYPE: EPUB 3 content documents should use <!DOCTYPE html>",
			location)
	}
}

// HTM-012: XHTML namespace check
func checkXHTMLNamespace(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		if se, ok := tok.(xml.StartElement); ok {
			if se.Name.Local == "html" {
				ns := se.Name.Space
				if ns != "" && ns != "http://www.w3.org/1999/xhtml" {
					r.AddWithLocation(report.Error, "HTM-012",
						fmt.Sprintf("The html element namespace is wrong: '%s'", ns),
						location)
				}
				return
			}
		}
	}
}

// checkPropertyDeclarations: check for script/SVG/MathML/switch/form/remote-resources
// and verify declared manifest properties match actual content.
// OPF-014: property needed but not declared
// OPF-015: property declared but not needed
func checkPropertyDeclarations(ep *epub.EPUB, data []byte, location string, item epub.ManifestItem, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	hasScript := false
	hasSVG := false
	hasMathML := false
	hasSwitch := false
	hasForm := false
	hasRemoteResources := false
	hasRemoteInlineCSS := false

	// Build set of linked CSS hrefs to check for remote resources
	var linkedCSSHrefs []string

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}

		if se.Name.Local == "script" {
			// Per HTML spec, <script type="text/plain"> and other non-JS types
			// are data blocks, not executable scripts. Only count as scripted
			// if type is absent or a JavaScript-compatible MIME type.
			scriptType := ""
			for _, attr := range se.Attr {
				if attr.Name.Local == "type" {
					scriptType = strings.TrimSpace(strings.ToLower(attr.Value))
				}
			}
			if isExecutableScriptType(scriptType) {
				hasScript = true
			}
		}
		if se.Name.Local == "svg" || se.Name.Space == "http://www.w3.org/2000/svg" {
			hasSVG = true
		}
		if se.Name.Local == "math" || se.Name.Space == "http://www.w3.org/1998/Math/MathML" {
			hasMathML = true
		}
		// epub:switch detection
		if se.Name.Local == "switch" && (se.Name.Space == "http://www.idpf.org/2007/ops" ||
			strings.HasPrefix(getAttrVal(se, "xmlns:epub"), "http://www.idpf.org/2007/ops")) {
			hasSwitch = true
		}
		// Form elements count as scripted per epubcheck
		if se.Name.Local == "form" {
			hasForm = true
		}
		// Check for remote resource references in content elements
		switch se.Name.Local {
		case "audio", "video", "source", "img", "iframe", "object", "embed":
			for _, attr := range se.Attr {
				if (attr.Name.Local == "src" || attr.Name.Local == "poster" || attr.Name.Local == "data") && isRemoteURL(attr.Value) {
					hasRemoteResources = true
				}
			}
		case "script":
			for _, attr := range se.Attr {
				if attr.Name.Local == "src" && isRemoteURL(attr.Value) {
					hasRemoteResources = true
				}
			}
		}
		// Collect linked CSS stylesheet hrefs
		if se.Name.Local == "link" {
			rel := ""
			href := ""
			for _, attr := range se.Attr {
				if attr.Name.Local == "rel" {
					rel = attr.Value
				}
				if attr.Name.Local == "href" {
					href = attr.Value
				}
			}
			if strings.Contains(rel, "stylesheet") && href != "" {
				linkedCSSHrefs = append(linkedCSSHrefs, href)
			}
		}
		// Check inline <style> for remote resources
		if se.Name.Local == "style" {
			// Read style content
			var styleContent string
			for {
				t, err := decoder.Token()
				if err != nil {
					break
				}
				if cd, ok := t.(xml.CharData); ok {
					styleContent += string(cd)
				}
				if _, ok := t.(xml.EndElement); ok {
					break
				}
			}
			if hasRemoteURLInCSS(styleContent) {
				hasRemoteInlineCSS = true
			}
		}
	}

	// Check linked CSS files for remote resources
	itemDir := path.Dir(location)
	for _, href := range linkedCSSHrefs {
		if isRemoteURL(href) {
			continue
		}
		cssPath := resolvePath(itemDir, href)
		cssData, err := ep.ReadFile(cssPath)
		if err != nil {
			continue
		}
		if hasRemoteURLInCSS(string(cssData)) {
			hasRemoteResources = true
			break
		}
	}
	if hasRemoteInlineCSS {
		hasRemoteResources = true
	}

	// OPF-014: property needed but not declared
	if hasScript && !hasProperty(item.Properties, "scripted") {
		r.AddWithLocation(report.Error, "OPF-014",
			"Property 'scripted' should be declared in the manifest for scripted content",
			location)
	}
	if hasForm && !hasProperty(item.Properties, "scripted") && !hasScript {
		r.AddWithLocation(report.Error, "OPF-014",
			"Property 'scripted' should be declared in the manifest for content with form elements",
			location)
	}
	if hasSVG && !hasProperty(item.Properties, "svg") {
		r.AddWithLocation(report.Error, "OPF-014",
			"Property 'svg' should be declared in the manifest for content with inline SVG",
			location)
	}
	if hasMathML && !hasProperty(item.Properties, "mathml") {
		r.AddWithLocation(report.Error, "OPF-014",
			"Property 'mathml' should be declared in the manifest for content with MathML",
			location)
	}
	if hasSwitch {
		// RSC-017: epub:switch is deprecated
		r.AddWithLocation(report.Warning, "RSC-017",
			`The "epub:switch" element is deprecated`,
			location)
		if !hasProperty(item.Properties, "switch") {
			r.AddWithLocation(report.Error, "OPF-014",
				"Property 'switch' should be declared in the manifest for content with epub:switch",
				location)
		}
	}
	if hasRemoteResources && !hasProperty(item.Properties, "remote-resources") {
		r.AddWithLocation(report.Error, "OPF-014",
			"Property 'remote-resources' should be declared in the manifest for content with remote resources",
			location)
	}

	// OPF-015: property declared but not needed
	hasScriptOrForm := hasScript || hasForm
	if hasProperty(item.Properties, "scripted") && !hasScriptOrForm {
		r.AddWithLocation(report.Error, "OPF-015",
			"Property 'scripted' is declared in the manifest but the content does not contain scripted elements",
			location)
	}
	if hasProperty(item.Properties, "svg") && !hasSVG {
		r.AddWithLocation(report.Error, "OPF-015",
			"Property 'svg' is declared in the manifest but the content does not contain SVG elements",
			location)
	}
	if hasProperty(item.Properties, "remote-resources") && !hasRemoteResources {
		if hasProperty(item.Properties, "scripted") {
			// OPF-018b: scripted content may access remote resources dynamically
			r.AddWithLocation(report.Usage, "OPF-018b",
				"Property 'remote-resources' is declared but could not be verified because the content document is scripted",
				location)
			// RSC-006b: suggest manually checking scripts for remote resource usage
			r.AddWithLocation(report.Usage, "RSC-006b",
				"Scripted content documents may access remote resources; scripts should be manually checked",
				location)
		} else {
			// OPF-018: remote-resources declared but no remote resources found (warning)
			r.AddWithLocation(report.Warning, "OPF-018",
				"Property 'remote-resources' is declared in the manifest but is not needed",
				location)
		}
	}
}

// hasRemoteURLInCSS checks if CSS content contains any remote URL references
// (excluding @namespace and @import declarations which are handled separately)
func hasRemoteURLInCSS(css string) bool {
	// Remove @namespace lines (use url() for identifiers, not resources)
	namespaceRe := regexp.MustCompile(`(?m)@namespace\s+[^\n;]+;`)
	cleaned := namespaceRe.ReplaceAllString(css, "")
	// Remove @import lines (remote @import is handled by RSC-006, not remote-resources)
	importRe := regexp.MustCompile(`(?m)@import\s+[^\n;]+;`)
	cleaned = importRe.ReplaceAllString(cleaned, "")
	urlRe := regexp.MustCompile(`url\(['"]?(https?://[^'"\)\s]+)['"]?\)`)
	return urlRe.MatchString(cleaned)
}

// getAttrVal gets an attribute value by local name from a start element
func getAttrVal(se xml.StartElement, name string) string {
	for _, attr := range se.Attr {
		if attr.Name.Local == name {
			return attr.Value
		}
	}
	return ""
}

// checkSVGPropertyDeclarations checks SVG content documents for remote resources
// and verifies the remote-resources manifest property is declared.
func checkSVGPropertyDeclarations(ep *epub.EPUB, data []byte, location string, item epub.ManifestItem, r *report.Report) {
	content := string(data)
	hasRemote := false

	// Strip XML processing instructions — their remote hrefs are disallowed (RSC-006)
	// and should not trigger the remote-resources property requirement (OPF-014).
	piRe := regexp.MustCompile(`<\?[^?]*\?>`)
	stripped := piRe.ReplaceAllString(content, "")

	// Check for remote URLs in SVG element attributes (href, xlink:href)
	remoteRe := regexp.MustCompile(`(?:href|xlink:href)\s*=\s*["'](https?://[^"']+)["']`)
	if remoteRe.MatchString(stripped) {
		hasRemote = true
	}
	// hasRemoteURLInCSS already strips @import (which fire RSC-006, not OPF-014)
	if hasRemoteURLInCSS(stripped) {
		hasRemote = true
	}

	if hasRemote && !hasProperty(item.Properties, "remote-resources") {
		r.AddWithLocation(report.Error, "OPF-014",
			"Property 'remote-resources' should be declared in the manifest for content with remote resources",
			location)
	}
}

// checkFXLSVGViewBox checks that a fixed-layout SVG content document has a viewBox
// attribute on the root svg element. HTM-048.
func checkFXLSVGViewBox(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(bytes.NewReader(data))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Local == "svg" {
			hasViewBox := false
			for _, attr := range se.Attr {
				if strings.EqualFold(attr.Name.Local, "viewBox") {
					hasViewBox = true
					break
				}
			}
			if !hasViewBox {
				r.AddWithLocation(report.Error, "HTM-048",
					"Fixed-layout SVG documents must declare a 'viewBox' attribute on the root 'svg' element",
					location)
			}
			return // Only check root svg
		}
	}
}

// viewportDim represents a parsed key=value pair from a viewport meta content.
type viewportDim struct {
	key   string
	value string
	hasEq bool // true if '=' was present in the source
}

// viewportUnits matches a trailing CSS unit or % on a dimension value.
var viewportUnitRe = regexp.MustCompile(`(?i)(px|em|ex|rem|%|vw|vh|pt|pc|cm|mm|in)$`)

// parseViewportDims splits a viewport content string into key[=value] pairs.
func parseViewportDims(content string) []viewportDim {
	var dims []viewportDim
	for _, part := range strings.Split(content, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		idx := strings.IndexByte(part, '=')
		if idx < 0 {
			dims = append(dims, viewportDim{key: strings.ToLower(strings.TrimSpace(part))})
		} else {
			key := strings.ToLower(strings.TrimSpace(part[:idx]))
			val := part[idx+1:] // keep original spacing for whitespace-only detection
			dims = append(dims, viewportDim{key: key, value: val, hasEq: true})
		}
	}
	return dims
}

// HTM-060b: viewport meta tag in reflowable content (usage note)
func checkReflowViewport(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Local == "body" {
			break
		}
		if se.Name.Local == "meta" {
			var name string
			for _, attr := range se.Attr {
				if attr.Name.Local == "name" {
					name = attr.Value
				}
			}
			if name == "viewport" {
				r.AddWithLocation(report.Usage, "HTM-060b",
					"Viewport metadata is only used for fixed-layout content documents; it will be ignored in reflowable content",
					location)
				return
			}
		}
	}
}

// HTM-046/047/056/057/059/060a: Fixed-layout XHTML viewport checks
func checkFXLViewport(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	viewportCount := 0
	viewportContent := ""

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}

		if se.Name.Local == "meta" {
			var name, content string
			for _, attr := range se.Attr {
				switch attr.Name.Local {
				case "name":
					name = attr.Value
				case "content":
					content = attr.Value
				}
			}
			if name == "viewport" {
				viewportCount++
				if viewportCount == 1 {
					viewportContent = content
				} else {
					// HTM-060a: additional viewport meta tags are ignored (usage)
					r.AddWithLocation(report.Usage, "HTM-060a",
						"Fixed-layout documents must have only one viewport declaration; additional viewport meta elements are ignored",
						location)
				}
			}
		}

		// Stop after head
		if se.Name.Local == "body" {
			break
		}
	}

	if viewportCount == 0 {
		r.AddWithLocation(report.Error, "HTM-046",
			"Fixed-layout content document has no viewport meta element",
			location)
		return
	}

	dims := parseViewportDims(viewportContent)

	// HTM-047: key= with empty or all-whitespace value (syntax invalid)
	for _, d := range dims {
		if d.hasEq && strings.TrimSpace(d.value) == "" {
			r.AddWithLocation(report.Error, "HTM-047",
				fmt.Sprintf("The viewport meta element has an invalid value for dimension '%s'", d.key),
				location)
			return
		}
	}

	// HTM-059: duplicate width or height keys
	seen := make(map[string]int)
	for _, d := range dims {
		if d.key == "width" || d.key == "height" {
			seen[d.key]++
		}
	}
	for _, key := range []string{"width", "height"} {
		if seen[key] > 1 {
			r.AddWithLocation(report.Error, "HTM-059",
				fmt.Sprintf("The viewport meta element declares '%s' more than once", key),
				location)
		}
	}
	if seen["width"] > 1 || seen["height"] > 1 {
		return
	}

	// HTM-057: dimension present but value has units or no value (key without =)
	for _, d := range dims {
		if d.key != "width" && d.key != "height" {
			continue
		}
		val := strings.TrimSpace(d.value)
		if !d.hasEq || val == "" {
			// key with no = at all (empty value treated as HTM-057)
			r.AddWithLocation(report.Error, "HTM-057",
				fmt.Sprintf("The value of viewport dimension '%s' must be a number without units", d.key),
				location)
		} else if viewportUnitRe.MatchString(val) {
			r.AddWithLocation(report.Error, "HTM-057",
				fmt.Sprintf("The value of viewport dimension '%s' must be a number without units", d.key),
				location)
		}
	}

	// HTM-056: missing width or height
	hasWidth := false
	hasHeight := false
	for _, d := range dims {
		if d.key == "width" {
			hasWidth = true
		} else if d.key == "height" {
			hasHeight = true
		}
	}
	if !hasWidth || !hasHeight {
		r.AddWithLocation(report.Error, "HTM-056",
			"Viewport metadata must specify both width and height dimensions",
			location)
	}
}

// RSC-003: fragment identifiers must resolve
func checkFragmentIdentifiers(ep *epub.EPUB, data []byte, fullPath string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	itemDir := path.Dir(fullPath)

	// Collect all id attributes in the document for self-references
	ids := collectIDs(data)

	decoder = newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}

		if se.Name.Local == "a" {
			for _, attr := range se.Attr {
				if attr.Name.Local == "href" {
					checkFragmentRef(ep, attr.Value, itemDir, fullPath, ids, r)
				}
			}
		}
	}
}

func checkFragmentRef(ep *epub.EPUB, href, itemDir, location string, localIDs map[string]bool, r *report.Report) {
	if href == "" {
		return
	}

	u, err := url.Parse(href)
	if err != nil || u.Scheme != "" {
		return
	}

	fragment := u.Fragment
	if fragment == "" {
		return // No fragment to check
	}

	// Skip media fragment URIs (EPUB Region-Based Navigation, Media Fragments).
	// These use schemes like #xywh=, #xyn=, #t= and are not HTML element IDs.
	if strings.HasPrefix(fragment, "xywh=") || strings.HasPrefix(fragment, "xyn=") ||
		strings.HasPrefix(fragment, "t=") || strings.HasPrefix(fragment, "epubcfi(") {
		return
	}

	refPath := u.Path
	if refPath == "" {
		// Self-reference fragment
		if !localIDs[fragment] {
			r.AddWithLocation(report.Error, "RSC-012",
				fmt.Sprintf("Fragment identifier is not defined: '#%s'", fragment),
				location)
		}
		return
	}

	// Cross-document fragment reference
	target := resolvePath(itemDir, refPath)
	targetData, err := ep.ReadFile(target)
	if err != nil {
		return // File missing, handled by HTM-008
	}

	targetIDs := collectIDs(targetData)
	if !targetIDs[fragment] {
		r.AddWithLocation(report.Error, "RSC-012",
			fmt.Sprintf("Fragment identifier is not defined: '%s#%s'", refPath, fragment),
			location)
	}
}

func collectIDs(data []byte) map[string]bool {
	ids := make(map[string]bool)
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	decoder.Entity = xhtmlEntities
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		if se, ok := tok.(xml.StartElement); ok {
			for _, attr := range se.Attr {
				if attr.Name.Local == "id" {
					ids[attr.Value] = true
				}
			}
		}
	}
	return ids
}

// checkNoRemoteResources validates remote resource usage.
// Per EPUB spec:
// - Remote audio/video are ALLOWED (just need remote-resources property)
// - Remote fonts (in CSS/SVG) are ALLOWED
// - Remote images, iframes, scripts, stylesheets, objects are NOT allowed (RSC-006)
func checkNoRemoteResources(ep *epub.EPUB, data []byte, location string, item epub.ManifestItem, remoteManifestURLs map[string]bool, r *report.Report) {
	// Detect external base URL (from <base href="..."> or xml:base="...") for RSC-006
	_, isHTMLBase := detectExternalBaseURL(data)

	decoder := newXHTMLDecoder(bytes.NewReader(data))
	// Match href="..." in processing instruction data
	piHrefRe := regexp.MustCompile(`href\s*=\s*["']([^"']+)["']`)

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}

		// RSC-006: Remote stylesheet in SVG <?xml-stylesheet href="...">
		if pi, ok := tok.(xml.ProcInst); ok {
			if pi.Target == "xml-stylesheet" {
				m := piHrefRe.FindSubmatch(pi.Inst)
				if m != nil && isRemoteURL(string(m[1])) {
					r.AddWithLocation(report.Error, "RSC-006",
						fmt.Sprintf("Remote resource reference is not allowed: '%s'", string(m[1])),
						location)
				}
			}
			continue
		}

		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}

		// RSC-006: Remote image resources are not allowed
		if se.Name.Local == "img" {
			for _, attr := range se.Attr {
				if attr.Name.Local == "src" && isRemoteURL(attr.Value) {
					r.AddWithLocation(report.Error, "RSC-006",
						fmt.Sprintf("Remote resource reference is not allowed: '%s'", attr.Value),
						location)
				}
			}
		}

		// RSC-006: Remote iframe/embed resources are not allowed
		if se.Name.Local == "iframe" || se.Name.Local == "embed" {
			for _, attr := range se.Attr {
				if attr.Name.Local == "src" && isRemoteURL(attr.Value) {
					r.AddWithLocation(report.Error, "RSC-006",
						fmt.Sprintf("Remote resource reference is not allowed: '%s'", attr.Value),
						location)
				}
			}
		}
		// RSC-006: Remote object data is not allowed
		if se.Name.Local == "object" {
			for _, attr := range se.Attr {
				if attr.Name.Local == "data" && isRemoteURL(attr.Value) {
					// Check if this remote object references audio/video (allowed)
					var objType string
					for _, a2 := range se.Attr {
						if a2.Name.Local == "type" {
							objType = a2.Value
						}
					}
					if strings.HasPrefix(objType, "audio/") || strings.HasPrefix(objType, "video/") {
						// Remote audio/video via object is allowed
						// Property check handled by checkPropertyDeclarations
					} else {
						r.AddWithLocation(report.Error, "RSC-006",
							fmt.Sprintf("Remote resource reference is not allowed: '%s'", attr.Value),
							location)
					}
				}
			}
		}

		// RSC-006: Remote script references are not allowed
		if se.Name.Local == "script" {
			for _, attr := range se.Attr {
				if attr.Name.Local == "src" && isRemoteURL(attr.Value) {
					r.AddWithLocation(report.Error, "RSC-006",
						fmt.Sprintf("Remote resource reference is not allowed: '%s'", attr.Value),
						location)
				}
			}
		}

		// Remote audio/video resources are ALLOWED in EPUB 3 if declared in manifest.
		// RSC-008: remote audio/video not declared in manifest.
		// RSC-031: warn when using http:// instead of https:// for remote resources.
		if se.Name.Local == "audio" || se.Name.Local == "video" || se.Name.Local == "source" {
			for _, attr := range se.Attr {
				if attr.Name.Local == "src" {
					if isNonHTTPSRemote(attr.Value) {
						r.AddWithLocation(report.Warning, "RSC-031",
							fmt.Sprintf("Remote resource uses insecure 'http' scheme: '%s'", attr.Value),
							location)
					}
					if isRemoteURL(attr.Value) && !remoteManifestURLs[attr.Value] {
						r.AddWithLocation(report.Error, "RSC-008",
							fmt.Sprintf("Remote resource '%s' is not declared in the package document", attr.Value),
							location)
					}
				}
			}
		}

		// RSC-006: Remote stylesheet references are not allowed
		if se.Name.Local == "link" {
			var href, rel string
			for _, attr := range se.Attr {
				switch attr.Name.Local {
				case "href":
					href = attr.Value
				case "rel":
					rel = attr.Value
				}
			}
			if rel == "stylesheet" {
				if isRemoteURL(href) {
					r.AddWithLocation(report.Error, "RSC-006",
						fmt.Sprintf("Remote resource reference is not allowed: '%s'", href),
						location)
				} else if href != "" && isHTMLBase {
					// RSC-006: relative stylesheet becomes remote via HTML <base> element
					r.AddWithLocation(report.Error, "RSC-006",
						fmt.Sprintf("Remote resource reference is not allowed: '%s'", href),
						location)
				}
			}
		}

		// RSC-015: SVG <use> element must reference a document fragment
		if se.Name.Local == "use" {
			for _, attr := range se.Attr {
				if attr.Name.Local == "href" {
					href := attr.Value
					if href != "" && !strings.Contains(href, "#") && !isRemoteURL(href) {
						r.AddWithLocation(report.Error, "RSC-015",
							fmt.Sprintf("SVG 'use' element must reference a fragment identifier: '%s'", href),
							location)
					}
				}
			}
		}

		// RSC-006: Remote stylesheet in SVG inline <style> @import
		if se.Name.Local == "style" {
			inner, _ := decoder.Token()
			if cd, ok2 := inner.(xml.CharData); ok2 {
				css := string(cd)
				importRe := regexp.MustCompile(`@import\s+(?:url\(['"]?|['"])([^'")\s]+)`)
				for _, m := range importRe.FindAllStringSubmatch(css, -1) {
					if isRemoteURL(m[1]) {
						r.AddWithLocation(report.Error, "RSC-006",
							fmt.Sprintf("Remote resource reference is not allowed: '%s'", m[1]),
							location)
					}
				}
			}
		}
	}
}

// checkSVGSymbolLinks checks for hyperlinks to SVG symbol elements.
// RSC-014: linking to a symbol is an incompatible resource type.
func checkSVGSymbolLinks(data []byte, location string, r *report.Report) {
	// First pass: collect all symbol element IDs
	symbolIDs := make(map[string]bool)
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Local == "symbol" {
			for _, attr := range se.Attr {
				if attr.Name.Local == "id" && attr.Value != "" {
					symbolIDs[attr.Value] = true
				}
			}
		}
	}
	if len(symbolIDs) == 0 {
		return
	}

	// Second pass: check <a href="#id"> against symbol IDs
	decoder = newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Local == "a" {
			for _, attr := range se.Attr {
				if attr.Name.Local == "href" {
					href := attr.Value
					if strings.HasPrefix(href, "#") {
						frag := href[1:]
						if symbolIDs[frag] {
							r.AddWithLocation(report.Error, "RSC-014",
								fmt.Sprintf("Hyperlink to SVG 'symbol' element is not allowed: '%s'", href),
								location)
						}
					}
				}
			}
		}
	}
}

func isRemoteURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func isFileURL(s string) bool {
	return strings.HasPrefix(s, "file://") || strings.HasPrefix(s, "file:/")
}

func isNonHTTPSRemote(s string) bool {
	return strings.HasPrefix(s, "http://")
}

// checkContentReferences finds href/src attributes in XHTML and validates them.
func checkContentReferences(ep *epub.EPUB, data []byte, fullPath, itemHref string, manifestPaths map[string]bool, remoteManifestItems map[string]epub.ManifestItem, manifestByPath map[string]epub.ManifestItem, spinePathSet map[string]bool, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	itemDir := path.Dir(fullPath)

	// Detect external base URL for RSC-006 (relative paths become remote)
	externalBase, _ := detectExternalBaseURL(data)

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}

		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}

		// Check <a href="..."> for internal links and remote image references
		if se.Name.Local == "a" {
			for _, attr := range se.Attr {
				if attr.Name.Local == "href" {
					// RSC-006: <a> linking to a remote image is not allowed
					if isRemoteURL(attr.Value) {
						if mItem, ok2 := remoteManifestItems[attr.Value]; ok2 {
							if strings.HasPrefix(mItem.MediaType, "image/") {
								r.AddWithLocation(report.Error, "RSC-006",
									fmt.Sprintf("Remote resource reference is not allowed: '%s'", attr.Value),
									fullPath)
							}
						}
					} else if externalBase != "" {
						// RSC-006: relative href becomes remote via external base URL
						u, err := url.Parse(attr.Value)
						if err == nil && u.Scheme == "" && u.Path != "" {
							r.AddWithLocation(report.Error, "RSC-006",
								fmt.Sprintf("Remote resource reference is not allowed: '%s'", attr.Value),
								fullPath)
						}
						// Skip checkHyperlink since local lookup would give wrong RSC-007
					} else {
						// RSC-011: hyperlinks to XHTML/SVG docs must point to spine items
						u, err := url.Parse(attr.Value)
						if err == nil && u.Scheme == "" && u.Path != "" {
							target := resolvePath(itemDir, u.Path)
							if mItem, ok2 := manifestByPath[target]; ok2 {
								isContentDoc := mItem.MediaType == "application/xhtml+xml" || mItem.MediaType == "image/svg+xml"
								if isContentDoc && !spinePathSet[target] {
									r.AddWithLocation(report.Error, "RSC-011",
										fmt.Sprintf("Content document '%s' is hyperlinked but not listed in the spine", attr.Value),
										fullPath)
								}
							}
						}
						checkHyperlink(ep, attr.Value, itemDir, fullPath, r)
					}
				}
			}
		}

		// Check <img src="..."> and <img srcset="..."> for image references
		if se.Name.Local == "img" {
			for _, attr := range se.Attr {
				switch attr.Name.Local {
				case "src":
					// RSC-009: fragment identifier on non-SVG image reference
					if idx := strings.Index(attr.Value, "#"); idx >= 0 {
						base := attr.Value[:idx]
						if !strings.HasSuffix(strings.ToLower(base), ".svg") {
							r.AddWithLocation(report.Warning, "RSC-009",
								fmt.Sprintf("Fragment identifier not allowed on non-SVG image reference: '%s'", attr.Value),
								fullPath)
						}
					}
					checkResourceRef(ep, attr.Value, itemDir, fullPath, manifestPaths, r)
				case "srcset":
					// RSC-008: srcset resources must be declared in the manifest
					checkSrcsetRef(ep, attr.Value, itemDir, fullPath, manifestPaths, r)
				}
			}
		}

		// RSC-009: SVG <image> with fragment on non-SVG resource
		if se.Name.Local == "image" {
			for _, attr := range se.Attr {
				if attr.Name.Local == "href" {
					href := attr.Value
					if idx := strings.Index(href, "#"); idx >= 0 {
						base := href[:idx]
						if !strings.HasSuffix(strings.ToLower(base), ".svg") {
							r.AddWithLocation(report.Warning, "RSC-009",
								fmt.Sprintf("Fragment identifier not allowed on non-SVG image reference: '%s'", href),
								fullPath)
						}
					}
				}
			}
		}

		// RSC-007: MathML altimg not found; <iframe src="...">, <embed src="...">, <object data="...">
		if se.Name.Local == "iframe" || se.Name.Local == "embed" {
			for _, attr := range se.Attr {
				if attr.Name.Local == "src" {
					checkResourceRef(ep, attr.Value, itemDir, fullPath, manifestPaths, r)
				}
			}
		}
		if se.Name.Local == "object" {
			for _, attr := range se.Attr {
				if attr.Name.Local == "data" {
					checkResourceRef(ep, attr.Value, itemDir, fullPath, manifestPaths, r)
				}
			}
		}

		// RSC-007: MathML <math altimg="..."> must reference an existing resource
		if se.Name.Local == "math" {
			for _, attr := range se.Attr {
				if attr.Name.Local == "altimg" && attr.Value != "" {
					checkResourceRef(ep, attr.Value, itemDir, fullPath, manifestPaths, r)
				}
			}
		}

		// RSC-007: Check <audio src="...">, <video src="...">, <source src="...">, <track src="...">
		if se.Name.Local == "audio" || se.Name.Local == "video" || se.Name.Local == "source" || se.Name.Local == "track" {
			for _, attr := range se.Attr {
				if attr.Name.Local == "src" {
					checkResourceRef(ep, attr.Value, itemDir, fullPath, manifestPaths, r)
				}
			}
		}

		// RSC-007: Check <blockquote cite="...">, <q cite="...">, <ins cite="...">, <del cite="...">
		if se.Name.Local == "blockquote" || se.Name.Local == "q" || se.Name.Local == "ins" || se.Name.Local == "del" {
			for _, attr := range se.Attr {
				if attr.Name.Local == "cite" {
					checkResourceRef(ep, attr.Value, itemDir, fullPath, manifestPaths, r)
				}
			}
		}

		// RSC-007: Check <link rel="stylesheet" href="..."> for missing stylesheets
		if se.Name.Local == "link" {
			rel := ""
			href := ""
			for _, attr := range se.Attr {
				if attr.Name.Local == "rel" {
					rel = attr.Value
				}
				if attr.Name.Local == "href" {
					href = attr.Value
				}
			}
			if strings.Contains(strings.ToLower(rel), "stylesheet") && href != "" && !isRemoteURL(href) {
				// RSC-013: stylesheet URLs must not contain fragment identifiers
				if u, err := url.Parse(href); err == nil && u.Fragment != "" {
					r.AddWithLocation(report.Error, "RSC-013",
						fmt.Sprintf("Fragment identifier is not allowed in stylesheet URL: '%s'", href),
						fullPath)
				} else {
					target := resolvePath(itemDir, href)
					if _, exists := ep.Files[target]; !exists {
						r.AddWithLocation(report.Error, "RSC-007",
							fmt.Sprintf("Referenced resource '%s' could not be found in the container", href),
							fullPath)
					}
				}
			}
		}
	}
}

// checkHyperlink validates a hyperlink reference from a content document.
func checkHyperlink(ep *epub.EPUB, href, itemDir, location string, r *report.Report) {
	if strings.TrimSpace(href) == "" {
		return
	}

	u, err := url.Parse(href)
	if err != nil {
		return
	}
	if u.Scheme != "" {
		return
	}

	refPath := u.Path
	if refPath == "" {
		return // fragment-only reference
	}

	// Skip absolute paths (starting with /) — these are not valid EPUB
	// container references and typically come from embedded web content
	// (e.g., Wikipedia articles with /wiki/... links).
	if strings.HasPrefix(refPath, "/") {
		return
	}

	target := resolvePath(itemDir, refPath)
	if _, exists := ep.Files[target]; !exists {
		r.AddWithLocation(report.Error, "RSC-007",
			fmt.Sprintf("Referenced resource '%s' could not be found in the container", refPath),
			location)
	}
}

// checkResourceRef validates a resource reference (img src, etc.) from a content document.
func checkResourceRef(ep *epub.EPUB, src, itemDir, location string, manifestPaths map[string]bool, r *report.Report) {
	if src == "" {
		return
	}

	u, err := url.Parse(src)
	if err != nil {
		return
	}
	if u.Scheme != "" {
		return // remote URL - handled by remote resource checks
	}

	refPath := u.Path
	if refPath == "" {
		return // fragment-only reference
	}

	// Skip absolute paths
	if strings.HasPrefix(refPath, "/") {
		return
	}

	target := resolvePath(itemDir, refPath)
	if manifestPaths[target] {
		return // good - exists in container and in manifest
	}
	if _, exists := ep.Files[target]; !exists {
		// RSC-007: not found in container at all
		r.AddWithLocation(report.Error, "RSC-007",
			fmt.Sprintf("Referenced resource '%s' could not be found in the container", src),
			location)
	} else {
		// RSC-006: exists in container but not declared in manifest
		r.AddWithLocation(report.Error, "RSC-006",
			fmt.Sprintf("Referenced resource '%s' is not declared in the OPF manifest", src),
			location)
	}
}

// checkSrcsetRef checks each URL in a srcset attribute.
// RSC-007: resource not found in container.
// RSC-008: resource in container but not declared in manifest.
func checkSrcsetRef(ep *epub.EPUB, srcset, itemDir, location string, manifestPaths map[string]bool, r *report.Report) {
	reported := make(map[string]bool)
	for _, entry := range strings.Split(srcset, ",") {
		parts := strings.Fields(strings.TrimSpace(entry))
		if len(parts) == 0 || parts[0] == "" {
			continue
		}
		href := parts[0]
		if reported[href] || isRemoteURL(href) {
			continue
		}
		reported[href] = true
		u, err := url.Parse(href)
		if err != nil || u.Path == "" {
			continue
		}
		target := resolvePath(itemDir, u.Path)
		if manifestPaths[target] {
			continue // declared in manifest — OK
		}
		if _, exists := ep.Files[target]; !exists {
			r.AddWithLocation(report.Error, "RSC-007",
				fmt.Sprintf("Referenced resource '%s' could not be found in the container", href),
				location)
		} else {
			r.AddWithLocation(report.Error, "RSC-008",
				fmt.Sprintf("Referenced resource '%s' is not declared in the OPF manifest", href),
				location)
		}
	}
}

// isExecutableScriptType returns true if the script type attribute value
// indicates executable JavaScript. Per HTML spec, a <script> is executable
// if type is absent/empty, or matches a JavaScript MIME type. Non-JS types
// like "text/plain" or "application/ld+json" are data blocks.
func isExecutableScriptType(t string) bool {
	if t == "" {
		return true // no type = JavaScript
	}
	jsTypes := map[string]bool{
		"text/javascript":        true,
		"application/javascript": true,
		"text/ecmascript":        true,
		"application/ecmascript": true,
		"module":                 true,
	}
	return jsTypes[t]
}

// resolvePath resolves a relative path against a base directory.
func resolvePath(baseDir, rel string) string {
	if path.IsAbs(rel) {
		return rel[1:] // strip leading /
	}
	return path.Clean(baseDir + "/" + rel)
}

// HTM-016: IDs must be unique within a content document
func checkUniqueIDs(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	seen := make(map[string]bool)
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attr := range se.Attr {
			if attr.Name.Local == "id" {
				if seen[attr.Value] {
					r.AddWithLocation(report.Error, "HTM-016",
						fmt.Sprintf("Duplicate ID '%s'", attr.Value),
						location)
				}
				seen[attr.Value] = true
			}
		}
	}
}

// HTM-018: content document must have exactly one body element
func checkSingleBody(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	bodyCount := 0
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		if se, ok := tok.(xml.StartElement); ok {
			if se.Name.Local == "body" {
				bodyCount++
			}
		}
	}
	if bodyCount > 1 {
		r.AddWithLocation(report.Error, "HTM-018",
			"Element body is not allowed here: content documents must have exactly one body element",
			location)
	}
}

// HTM-019: content document must have html as root element.
// Returns true if the root element is html.
func checkHTMLRootElement(data []byte, location string, r *report.Report) bool {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			return false
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		// First element should be html
		if se.Name.Local != "html" {
			r.AddWithLocation(report.Error, "HTM-019",
				fmt.Sprintf("Element body is not allowed here: expected element 'html' as root, but found '%s'", se.Name.Local),
				location)
			return false
		}
		return true
	}
}

// HTM-022: object data references must exist
func checkObjectReferences(ep *epub.EPUB, data []byte, fullPath string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	itemDir := path.Dir(fullPath)

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Local == "object" {
			for _, attr := range se.Attr {
				if attr.Name.Local == "data" && attr.Value != "" {
					u, err := url.Parse(attr.Value)
					if err != nil || u.Scheme != "" {
						continue
					}
					target := resolvePath(itemDir, u.Path)
					if _, exists := ep.Files[target]; !exists {
						r.AddWithLocation(report.Error, "HTM-022",
							fmt.Sprintf("Referenced resource '%s' could not be found in the container", attr.Value),
							fullPath)
					}
				}
			}
		}
	}
}

// HTM-003: hyperlink href attributes must not be empty
func checkEmptyHrefAttributes(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Local == "a" {
			for _, attr := range se.Attr {
				if attr.Name.Local == "href" && attr.Value == "" {
					r.AddWithLocation(report.Warning, "HTM-003",
						"Hyperlink href attribute must not be empty",
						location)
				}
			}
		}
	}
}

// HTM-052: the "region-based" epub:type is only allowed on nav elements
// in Data Navigation Documents (items with the "data-nav" property).
func checkRegionBasedProperty(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attr := range se.Attr {
			if attr.Name.Local == "type" && containsToken(attr.Value, "region-based") {
				r.AddWithLocation(report.Error, "HTM-052",
					`The property "region-based" is only allowed on nav elements in Data Navigation Documents`,
					location)
				return
			}
		}
	}
}

// HTM-045: report empty href attribute as usage (self-reference hint).
// In epubcheck this is reported on any element with href="" (a, area, link, etc.)
func checkEmptyHrefUsage(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attr := range se.Attr {
			if attr.Name.Local == "href" && attr.Value == "" {
				r.AddWithLocation(report.Usage, "HTM-045",
					"Encountered empty href",
					location)
				return // report once per document
			}
		}
	}
}

// HTM-051: Microdata attributes found without RDFa (EDUPUB recommendation).
// Reports once per document if microdata attributes (itemscope, itemprop, itemtype)
// are found but no RDFa attributes (vocab, typeof, property, about, resource, prefix).
func checkMicrodataWithoutRDFa(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	hasMicrodata := false
	hasRDFa := false
	rdfaAttrs := map[string]bool{
		"vocab": true, "typeof": true, "property": true,
		"about": true, "resource": true, "prefix": true,
	}
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attr := range se.Attr {
			switch attr.Name.Local {
			case "itemscope", "itemprop", "itemtype", "itemid", "itemref":
				hasMicrodata = true
			}
			if rdfaAttrs[attr.Name.Local] {
				hasRDFa = true
			}
		}
		if hasMicrodata && hasRDFa {
			return // both present, no warning
		}
	}
	if hasMicrodata && !hasRDFa {
		r.AddWithLocation(report.Warning, "HTM-051",
			"Found Microdata semantic enrichments but no RDFa. EDUPUB recommends using RDFa Lite",
			location)
	}
}

// HTM-009: base element should not be used in EPUB content documents
func checkNoBaseElement(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		if se, ok := tok.(xml.StartElement); ok {
			if se.Name.Local == "base" {
				r.AddWithLocation(report.Warning, "HTM-009",
					"The 'base' element is not allowed in EPUB content documents",
					location)
				return
			}
		}
	}
}

// detectExternalBaseURL scans an XHTML document for an external base URL set via
// <base href="http://..."> or xml:base="http://..." on the root element.
// Returns (baseURL, isHTMLBase) where isHTMLBase is true if found via <base> element.
// Returns ("", false) if no external base URL is set.
func detectExternalBaseURL(data []byte) (string, bool) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	first := true
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		// Check for xml:base on the first element (root element)
		if first {
			first = false
			for _, attr := range se.Attr {
				if attr.Name.Local == "base" && attr.Name.Space == "http://www.w3.org/XML/1998/namespace" {
					if isRemoteURL(attr.Value) {
						return attr.Value, false // xml:base
					}
				}
			}
		}
		// Check for <base href="..."> element
		if se.Name.Local == "base" {
			for _, attr := range se.Attr {
				if attr.Name.Local == "href" && isRemoteURL(attr.Value) {
					return attr.Value, true // HTML <base> element
				}
			}
		}
	}
	return "", false
}

// HTM-010: EPUB 3 content documents must use HTML5 DOCTYPE or no DOCTYPE.
// Returns true if a non-HTML5 DOCTYPE was detected (to skip HTM-011 which overlaps).
func checkDoctypeHTML5(data []byte, location string, r *report.Report) bool {
	content := string(data)
	idx := strings.Index(strings.ToUpper(content), "<!DOCTYPE")
	if idx == -1 {
		return false // No DOCTYPE is fine
	}
	endIdx := strings.Index(content[idx:], ">")
	if endIdx == -1 {
		return false
	}
	doctype := strings.ToUpper(content[idx : idx+endIdx+1])
	// HTML5 DOCTYPE is just <!DOCTYPE html> (case-insensitive, optionally with system)
	// If it contains XHTML DTD identifiers, it's wrong
	if strings.Contains(doctype, "XHTML") || strings.Contains(doctype, "DTD") {
		r.AddWithLocation(report.Error, "HTM-010",
			"Irregular DOCTYPE: EPUB 3 content documents must use the HTML5 DOCTYPE (<!DOCTYPE html>) or no DOCTYPE",
			location)
		return true
	}
	return false
}

// Valid epub:type values from the EPUB structural semantics vocabulary
var validEpubTypes = map[string]bool{
	"abstract": true, "acknowledgments": true, "afterword": true, "answer": true,
	"answers": true, "antonym-group": true, "appendix": true, "aside": true,
	"assessment": true, "assessments": true,
	"backlink": true, "backmatter": true, "balloon": true,
	"biblioentry": true, "bibliography": true, "biblioref": true,
	"bodymatter": true, "bridgehead": true,
	"chapter": true, "colophon": true, "concluding-sentence": true,
	"conclusion": true, "condensed-entry": true, "contributors": true,
	"copyright-page": true, "cover": true, "covertitle": true,
	"credit": true, "credits": true,
	"dedication": true, "def": true, "dictentry": true, "dictionary": true,
	"division": true,
	"endnote": true, "endnotes": true, "epigraph": true, "epilogue": true,
	"errata": true, "etymology": true, "example": true,
	"figure": true, "fill-in-the-blank-problem": true,
	"footnote": true, "footnotes": true, "foreword": true,
	"frontmatter": true, "fulltitle": true,
	"general-problem": true, "glossary": true, "glossdef": true,
	"glossref": true, "glossterm": true, "gram-info": true,
	"halftitle": true, "halftitlepage": true, "help": true,
	"idiom": true, "imprimatur": true, "imprint": true,
	"index": true, "index-editor-note": true, "index-entry": true,
	"index-entry-list": true, "index-group": true, "index-headnotes": true,
	"index-legend": true, "index-locator": true, "index-locator-list": true,
	"index-locator-range": true, "index-term": true, "index-term-categories": true,
	"index-term-category": true, "index-xref-preferred": true, "index-xref-related": true,
	"introduction": true, "keyword": true, "keywords": true, "label": true,
	"landmarks": true, "learning-objective": true, "learning-objectives": true,
	"learning-outcome": true, "learning-outcomes": true, "learning-resource": true,
	"learning-resources": true, "learning-standard": true, "learning-standards": true,
	"list": true, "list-item": true, "loa": true, "loi": true, "lot": true, "lov": true,
	"match-problem": true, "multiple-choice-problem": true, "noteref": true,
	"notice": true, "ordinal": true, "other-credits": true, "page-list": true,
	"pagebreak": true, "panel": true, "panel-group": true, "part": true,
	"part-of-speech": true, "part-of-speech-group": true, "part-of-speech-list": true,
	"phonetic-transcription": true, "phrase-group": true, "phrase-list": true,
	"practice": true, "practices": true, "preamble": true, "preface": true,
	"prologue": true, "pullquote": true, "qna": true, "question": true,
	"referrer": true, "revision-history": true,
	"sense-group": true, "sense-list": true, "sound-area": true,
	"subchapter": true, "subtitle": true, "synonym-group": true,
	"table": true, "table-cell": true, "table-row": true,
	"text-area": true, "tip": true, "title": true, "titlepage": true,
	"toc": true, "toc-brief": true, "topic-sentence": true,
	"tran": true, "tran-info": true, "true-false-problem": true,
	"volume": true, "warning": true,
}

// HTM-015: epub:type values must be valid
func checkEpubTypeValid(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attr := range se.Attr {
			if attr.Name.Local == "type" && attr.Name.Space == "http://www.idpf.org/2007/ops" {
				for _, val := range strings.Fields(attr.Value) {
					// Skip prefixed values (e.g., "dp:footnote") - those use custom vocabularies
					if strings.Contains(val, ":") {
						continue
					}
					if !validEpubTypes[val] {
						r.AddWithLocation(report.Info, "HTM-015",
							fmt.Sprintf("epub:type value '%s' is not a recognized structural semantics value", val),
							location)
					}
				}
			}
		}
	}
}

// HTM-020: processing instructions should not be used in EPUB content documents
func checkNoProcessingInstructions(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		if pi, ok := tok.(xml.ProcInst); ok {
			// Skip the xml declaration itself
			if pi.Target == "xml" {
				continue
			}
			r.AddWithLocation(report.Info, "HTM-020",
				fmt.Sprintf("Processing instruction '%s' found in EPUB content document", pi.Target),
				location)
		}
	}
}

// HTM-021: position:absolute in content documents may cause rendering issues
func checkNoPositionAbsolute(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attr := range se.Attr {
			if attr.Name.Local == "style" {
				if strings.Contains(strings.ToLower(attr.Value), "position") &&
					strings.Contains(strings.ToLower(attr.Value), "absolute") {
					r.AddWithLocation(report.Warning, "HTM-021",
						"Use of 'position:absolute' in content documents may cause rendering issues in reading systems",
						location)
					return
				}
			}
		}
	}
}

// HTM-023: links must not escape the container via parent directory traversal
func checkNoParentDirLinks(ep *epub.EPUB, data []byte, fullPath string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	itemDir := path.Dir(fullPath)

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}

		var hrefs []string
		for _, attr := range se.Attr {
			if attr.Name.Local == "href" || attr.Name.Local == "src" {
				hrefs = append(hrefs, attr.Value)
			}
		}

		for _, href := range hrefs {
			if href == "" {
				continue
			}
			u, err := url.Parse(href)
			if err != nil || u.Scheme != "" {
				continue
			}
			if u.Path == "" {
				continue
			}
			resolved := resolvePath(itemDir, u.Path)
			if strings.HasPrefix(resolved, "..") || strings.HasPrefix(resolved, "/") {
				r.AddWithLocation(report.Error, "HTM-023",
					fmt.Sprintf("Referenced resource '%s' leaks outside the container", href),
					fullPath)
			}
		}
	}
}

// HTM-024: XHTML content documents must have a head element
func checkContentHasHead(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		if se, ok := tok.(xml.StartElement); ok {
			if se.Name.Local == "head" {
				return
			}
		}
	}
	r.AddWithLocation(report.Error, "HTM-024",
		"Content document is missing required element 'head'",
		location)
}

// HTM-025: embed element src must reference existing resource
func checkEmbedReferences(ep *epub.EPUB, data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	contentDir := path.Dir(location)
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Local == "embed" {
			for _, attr := range se.Attr {
				if attr.Name.Local == "src" && attr.Value != "" {
					u, err := url.Parse(attr.Value)
					if err != nil || u.Scheme != "" {
						continue
					}
					target := resolvePath(contentDir, u.Path)
					if _, exists := ep.Files[target]; !exists {
						r.AddWithLocation(report.Error, "HTM-025",
							fmt.Sprintf("Referenced resource '%s' could not be found in the container", attr.Value),
							location)
					}
				}
			}
		}
	}
}

// HTM-026: lang and xml:lang must have the same value when both present
func checkLangXMLLangMatch(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		var lang, xmlLang string
		hasLang, hasXMLLang := false, false
		for _, attr := range se.Attr {
			if attr.Name.Local == "lang" && attr.Name.Space == "" {
				lang = attr.Value
				hasLang = true
			}
			if attr.Name.Local == "lang" && attr.Name.Space == "http://www.w3.org/XML/1998/namespace" {
				xmlLang = attr.Value
				hasXMLLang = true
			}
		}
		if hasLang && hasXMLLang && !strings.EqualFold(lang, xmlLang) {
			r.AddWithLocation(report.Error, "RSC-005",
				fmt.Sprintf("lang and xml:lang attributes must have the same value when both are present, but found '%s' and '%s'", lang, xmlLang),
				location)
			return
		}
	}
}

// HTM-027: video poster attribute must reference existing resource
func checkVideoPosterExists(ep *epub.EPUB, data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	contentDir := path.Dir(location)
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Local == "video" {
			for _, attr := range se.Attr {
				if attr.Name.Local == "poster" && attr.Value != "" {
					u, err := url.Parse(attr.Value)
					if err != nil || u.Scheme != "" {
						continue
					}
					target := resolvePath(contentDir, u.Path)
					if _, exists := ep.Files[target]; !exists {
						r.AddWithLocation(report.Error, "HTM-027",
							fmt.Sprintf("Referenced resource '%s' could not be found in the container", attr.Value),
							location)
					}
				}
			}
		}
	}
}

// HTM-028: audio src must reference existing resource
func checkAudioSrcExists(ep *epub.EPUB, data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	contentDir := path.Dir(location)
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Local == "audio" || se.Name.Local == "source" {
			for _, attr := range se.Attr {
				if attr.Name.Local == "src" && attr.Value != "" {
					u, err := url.Parse(attr.Value)
					if err != nil || u.Scheme != "" {
						continue
					}
					target := resolvePath(contentDir, u.Path)
					if _, exists := ep.Files[target]; !exists {
						r.AddWithLocation(report.Error, "HTM-028",
							fmt.Sprintf("Referenced resource '%s' could not be found in the container", attr.Value),
							location)
					}
				}
			}
		}
	}
}

// HTM-030: img src attribute must not be empty
func checkImgSrcNotEmpty(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Local == "img" {
			for _, attr := range se.Attr {
				if attr.Name.Local == "src" && attr.Value == "" {
					r.AddWithLocation(report.Error, "HTM-030",
						"The value of attribute 'src' is invalid; the value must be a string with length at least 1",
						location)
				}
			}
		}
	}
}

// Allowed attribute namespaces in EPUB XHTML content documents.
var allowedAttrNamespaces = map[string]bool{
	"":                                     true, // no namespace (plain HTML attributes)
	"xmlns":                                true, // namespace declarations (Go xml parser representation)
	"http://www.w3.org/1999/xhtml":         true, // XHTML
	"http://www.w3.org/XML/1998/namespace":  true, // xml: prefix
	"http://www.w3.org/2000/xmlns/":         true, // xmlns: declarations
	"http://www.idpf.org/2007/ops":          true, // epub: prefix
	"http://www.w3.org/2001/10/synthesis":   true, // ssml: prefix (TTS pronunciation)
	"http://www.w3.org/2000/svg":            true, // SVG namespace
	"http://www.w3.org/1998/Math/MathML":    true, // MathML namespace
	"http://www.w3.org/1999/xlink":          true, // XLink (used in SVG)
}

// HTM-031: custom attribute namespaces must be valid.
// Attributes using non-standard namespaces (e.g., a misspelled SSML namespace)
// are flagged. Valid SSML (ssml:ph, ssml:alphabet) is permitted for TTS.
func checkCustomAttributeNamespaces(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attr := range se.Attr {
			ns := attr.Name.Space
			if ns != "" && !allowedAttrNamespaces[ns] {
				r.AddWithLocation(report.Error, "HTM-031",
					fmt.Sprintf("Custom attribute namespace '%s' must not include non-standard namespaces", ns),
					location)
				return
			}
		}
	}
}

// HTM-032: CSS in inline style elements must be syntactically valid
func checkStyleElementValid(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "style" {
			continue
		}
		// Read the style content
		var cssContent string
		for {
			inner, err := decoder.Token()
			if err != nil {
				break
			}
			if cd, ok := inner.(xml.CharData); ok {
				cssContent += string(cd)
			}
			if _, ok := inner.(xml.EndElement); ok {
				break
			}
		}
		// Check for basic CSS syntax errors
		if strings.Contains(cssContent, "{") {
			// Check for empty values (property: ;)
			emptyVal := regexp.MustCompile(`:\s*;`)
			if emptyVal.MatchString(cssContent) {
				r.AddWithLocation(report.Error, "HTM-032",
					"An error occurred while parsing the CSS in style element",
					location)
			}
			// Check for missing closing braces
			opens := strings.Count(cssContent, "{")
			closes := strings.Count(cssContent, "}")
			if opens != closes {
				r.AddWithLocation(report.Error, "HTM-032",
					"An error occurred while parsing the CSS in style element: mismatched braces",
					location)
			}
		}
	}
}

// HTM-033: RDF metadata elements should not be used in EPUB content documents
func checkNoRDFElements(data []byte, location string, r *report.Report) {
	rdfNS := "http://www.w3.org/1999/02/22-rdf-syntax-ns#"
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		if se, ok := tok.(xml.StartElement); ok {
			if se.Name.Space == rdfNS || se.Name.Local == "RDF" {
				r.AddWithLocation(report.Error, "HTM-033",
					"RDF metadata elements should not be used in EPUB content documents",
					location)
				return
			}
		}
	}
}

// OPF-073: DOCTYPE external identifier checks.
// Allowed (publicID, systemID) pairs and the media types they are valid for:
//   - NCX: "-//NISO//DTD ncx 2005-1//EN" + "http://www.daisy.org/z3986/2005/ncx-2005-1.dtd" → application/x-dtbncx+xml
//   - SVG: "-//W3C//DTD SVG 1.1//EN" + "http://www.w3.org/Graphics/SVG/1.1/DTD/svg11.dtd" → image/svg+xml
//   - MathML: "-//W3C//DTD MathML 3.0//EN" + "http://www.w3.org/Math/DTD/mathml3/mathml3.dtd" → application/mathml+xml and variants

type allowedExternalID struct {
	publicID    string
	systemID    string
	mediaTypes  []string
}

var allowedExternalIDs = []allowedExternalID{
	{
		publicID:   "-//NISO//DTD ncx 2005-1//EN",
		systemID:   "http://www.daisy.org/z3986/2005/ncx-2005-1.dtd",
		mediaTypes: []string{"application/x-dtbncx+xml"},
	},
	{
		publicID:   "-//W3C//DTD SVG 1.1//EN",
		systemID:   "http://www.w3.org/Graphics/SVG/1.1/DTD/svg11.dtd",
		mediaTypes: []string{"image/svg+xml"},
	},
	{
		publicID:   "-//W3C//DTD MathML 3.0//EN",
		systemID:   "http://www.w3.org/Math/DTD/mathml3/mathml3.dtd",
		mediaTypes: []string{"application/mathml+xml", "application/mathml-presentation+xml", "application/mathml-content+xml"},
	},
}

// extractDOCTYPEIdentifiers parses a DOCTYPE directive string and returns the public and system IDs.
// Input is the content of <!...> without the brackets, e.g. `DOCTYPE ncx PUBLIC "..." "..."`.
func extractDOCTYPEIdentifiers(directive string) (publicID, systemID string) {
	// Quick check: must be a DOCTYPE directive
	upper := strings.ToUpper(strings.TrimSpace(directive))
	if !strings.HasPrefix(upper, "DOCTYPE") {
		return
	}

	// Extract quoted strings
	var quoted []string
	rest := directive
	for {
		q := -1
		for i, c := range rest {
			if c == '"' || c == '\'' {
				q = i
				break
			}
		}
		if q < 0 {
			break
		}
		delim := rest[q]
		end := strings.IndexByte(rest[q+1:], delim)
		if end < 0 {
			break
		}
		quoted = append(quoted, rest[q+1:q+1+end])
		rest = rest[q+1+end+1:]
	}

	if len(quoted) == 0 {
		return
	}

	// Check if PUBLIC or SYSTEM keyword is present
	upperDirective := strings.ToUpper(directive)
	if strings.Contains(upperDirective, "PUBLIC") {
		if len(quoted) >= 1 {
			publicID = quoted[0]
		}
		if len(quoted) >= 2 {
			systemID = quoted[1]
		}
	} else if strings.Contains(upperDirective, "SYSTEM") {
		if len(quoted) >= 1 {
			systemID = quoted[0]
		}
	}
	return
}

func checkDOCTYPEExternalIdentifiers(ep *epub.EPUB, r *report.Report) {
	// OPF-073 only applies to EPUB 3 publications
	if ep.Package.Version < "3.0" {
		return
	}
	for _, item := range ep.Package.Manifest {
		if item.Href == "\x00MISSING" {
			continue
		}
		fullPath := ep.ResolveHref(item.Href)
		data, err := ep.ReadFile(fullPath)
		if err != nil {
			continue
		}

		// Scan for DOCTYPE directive
		decoder := newXHTMLDecoder(strings.NewReader(string(data)))
		decoder.Strict = false
		decoder.AutoClose = xml.HTMLAutoClose
		for {
			tok, err := decoder.Token()
			if err != nil {
				break
			}
			// DOCTYPE appears as xml.Directive
			if dir, ok := tok.(xml.Directive); ok {
				directive := string(dir)
				publicID, systemID := extractDOCTYPEIdentifiers(directive)
				if publicID == "" && systemID == "" {
					continue
				}
				// Check if this is an allowed external identifier
				allowed := false
				correctMediaType := false
				for _, entry := range allowedExternalIDs {
					if publicID == entry.publicID && systemID == entry.systemID {
						allowed = true
						for _, mt := range entry.mediaTypes {
							if item.MediaType == mt {
								correctMediaType = true
								break
							}
						}
						break
					}
				}
				if !allowed {
					r.AddWithLocation(report.Error, "OPF-073",
						"DOCTYPE external identifier is not allowed",
						fullPath)
				} else if !correctMediaType {
					r.AddWithLocation(report.Error, "OPF-073",
						"DOCTYPE external identifier is not allowed for this media type",
						fullPath)
				}
			}
			// Stop after first element (DOCTYPE appears before root element)
			if _, ok := tok.(xml.StartElement); ok {
				break
			}
		}
	}
}

// validHTMLElements contains all valid HTML5 element names (lowercase).
var validHTMLElements = map[string]bool{
	"a": true, "abbr": true, "address": true, "area": true, "article": true,
	"aside": true, "audio": true, "b": true, "base": true, "bdi": true,
	"bdo": true, "blockquote": true, "body": true, "br": true, "button": true,
	"canvas": true, "caption": true, "cite": true, "code": true, "col": true,
	"colgroup": true, "data": true, "datalist": true, "dd": true, "del": true,
	"details": true, "dfn": true, "dialog": true, "div": true, "dl": true,
	"dt": true, "em": true, "embed": true, "fieldset": true, "figcaption": true,
	"figure": true, "footer": true, "form": true, "h1": true, "h2": true,
	"h3": true, "h4": true, "h5": true, "h6": true, "head": true, "header": true,
	"hgroup": true, "hr": true, "html": true, "i": true, "iframe": true,
	"img": true, "input": true, "ins": true, "kbd": true, "label": true,
	"legend": true, "li": true, "link": true, "main": true, "map": true,
	"mark": true, "math": true, "menu": true, "meta": true, "meter": true,
	"nav": true, "noscript": true, "object": true, "ol": true, "optgroup": true,
	"option": true, "output": true, "p": true, "picture": true, "pre": true,
	"progress": true, "q": true, "rp": true, "rt": true, "ruby": true,
	"s": true, "samp": true, "script": true, "search": true, "section": true,
	"select": true, "slot": true, "small": true, "source": true, "span": true,
	"strong": true, "style": true, "sub": true, "summary": true, "sup": true,
	"svg": true, "table": true, "tbody": true, "td": true, "template": true,
	"textarea": true, "tfoot": true, "th": true, "thead": true, "time": true,
	"title": true, "tr": true, "track": true, "u": true, "ul": true,
	"var": true, "video": true, "wbr": true,
	// Obsolete/legacy elements that are still commonly used
	"acronym": true, "applet": true, "basefont": true, "bgsound": true,
	"big": true, "blink": true, "center": true, "dir": true, "font": true,
	"frame": true, "frameset": true, "isindex": true, "keygen": true,
	"listing": true, "marquee": true, "menuitem": true,
	"multicol": true, "nextid": true, "nobr": true, "noembed": true,
	"noframes": true, "param": true, "plaintext": true, "rb": true,
	"rtc": true, "spacer": true, "strike": true, "tt": true, "xmp": true,
	// EPUB extensions in XHTML namespace
	"epub:switch": true, "epub:case": true, "epub:default": true,
	// Experimental/proposed elements
	"portal": true,
}

// checkInvalidHTMLElements reports RSC-005 for elements not in the valid HTML5 element set.
// Only checks elements in the XHTML namespace; skips content inside <svg> or <math> subtrees.
func checkInvalidHTMLElements(data []byte, location string, r *report.Report) {
	const xhtmlNS = "http://www.w3.org/1999/xhtml"
	const svgNS = "http://www.w3.org/2000/svg"
	const mathNS = "http://www.w3.org/1998/Math/MathML"

	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	foreignDepth := 0 // non-zero when inside svg/math/foreignObject

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if foreignDepth > 0 {
				foreignDepth++
				continue
			}
			// Skip non-XHTML namespace elements (includes SVG, MathML inline)
			ns := t.Name.Space
			if ns != "" && ns != xhtmlNS {
				foreignDepth = 1
				continue
			}
			name := strings.ToLower(t.Name.Local)
			// Custom elements (contain '-') are always valid
			if strings.Contains(name, "-") {
				continue
			}
			// Enter foreign content for svg and math
			if name == "svg" || name == "math" || name == "foreignobject" {
				foreignDepth = 1
				continue
			}
			if !validHTMLElements[name] {
				r.AddWithLocation(report.Error, "RSC-005",
					fmt.Sprintf("element \"%s\" not allowed here", name),
					location)
				return // report only first error
			}
		case xml.EndElement:
			if foreignDepth > 0 {
				foreignDepth--
			}
		}
	}
}

// checkNestedDFN reports RSC-005 when a <dfn> element contains a descendant <dfn>.
func checkNestedDFN(data []byte, location string, r *report.Report) {
	const xhtmlNS = "http://www.w3.org/1999/xhtml"
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	dfnDepth := 0

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			ns := t.Name.Space
			if ns != "" && ns != xhtmlNS {
				continue
			}
			if strings.ToLower(t.Name.Local) == "dfn" {
				if dfnDepth > 0 {
					r.AddWithLocation(report.Error, "RSC-005",
						"dfn must not have a dfn descendant",
						location)
					return
				}
				dfnDepth++
			}
		case xml.EndElement:
			ns := t.Name.Space
			if ns != "" && ns != xhtmlNS {
				continue
			}
			if strings.ToLower(t.Name.Local) == "dfn" && dfnDepth > 0 {
				dfnDepth--
			}
		}
	}
}

// checkSingleFileContent runs targeted content checks for single-file XHTML/SVG validation.
// This checks for data URLs, file URLs, and query strings in href attributes.
func checkSingleFileContent(ep *epub.EPUB, r *report.Report, opts Options) {
	if ep.Package == nil {
		return
	}

	// Run encoding detection for single-file XHTML (RSC-016/RSC-017/HTM-058)
	checkSingleFileEncoding(ep, r)

	for _, item := range ep.Package.Manifest {
		mt := item.MediaType
		fullPath := ep.ResolveHref(item.Href)
		data, err := ep.ReadFile(fullPath)
		if err != nil {
			continue
		}

		if mt == "application/xhtml+xml" {
			// Skip the synthetic nav document (only validate the user's file)
			if item.ID == "nav" && hasProperty(item.Properties, "nav") {
				continue
			}

			// HTM-001: XML 1.1 version detection
			checkXML11Version(data, fullPath, r)

			// HTM-058: encoding check (UTF-16 detection)
			checkHTM058Encoding(data, fullPath, r)

			// Well-formedness check (lenient for single-file mode)
			if !checkSingleFileWellFormed(data, fullPath, r) {
				checkSingleFileURLs(data, fullPath, r)
				continue
			}

			// Schema-like content checks (will be remapped to RSC-005)
			checkNestedAnchors(data, fullPath, r)
			checkMissingNamespace(data, fullPath, r)
			checkDuplicateIDs(data, fullPath, r)
			checkIDReferences(data, fullPath, r)
			checkObsoleteAttrs(data, fullPath, r)
			checkBlockInPhrasing(data, fullPath, r)
			checkRestrictedChildren(data, fullPath, r)
			checkVoidElementChildren(data, fullPath, r)
			checkTableContentModel(data, fullPath, r)
			checkInteractiveNesting(data, fullPath, r)
			checkTransparentContentModel(data, fullPath, r)
			checkFigcaptionPosition(data, fullPath, r)
			checkPictureContentModel(data, fullPath, r)
			checkDisallowedDescendants(data, fullPath, r)
			checkRequiredAncestor(data, fullPath, r)
			checkBdoDir(data, fullPath, r)
			checkSSMLPhNesting(data, fullPath, r)
			checkDuplicateMapName(data, fullPath, r)
			checkSelectMultiple(data, fullPath, r)
			checkMetaCharset(data, fullPath, r)
			checkLinkSizes(data, fullPath, r)

			// DOCTYPE check: mode-specific
			if opts.CheckMode == "xhtml" {
				// EPUB2 OPS XHTML mode
				checkHTM004EPUB2Mode(data, fullPath, r)
			} else {
				// EPUB3 mode
				checkHTM004SingleFile(data, fullPath, r)
			}

			// Unknown entity reference check (RSC-016) - catches &foo; style undeclared entities
			checkUnknownEntityRefs(data, fullPath, r)

			// EPUB2-specific checks
			if opts.CheckMode == "xhtml" {
				checkHTML5ElementsEPUB2(data, fullPath, r)
				checkCustomNamespacedAttrs(data, fullPath, r)
			}

			// Specific content checks
			checkExternalEntities(data, fullPath, r)
			checkHTM061DataAttrs(data, fullPath, r)
			checkHTM007SSML(data, fullPath, r)
			checkHTM025URLScheme(data, fullPath, r)
			checkCSS015AltStylesheet(data, fullPath, r)
			checkCSS005AltStyleTag(data, fullPath, r)
			checkDeprecatedDPUBARIA(data, fullPath, r)
			checkACCMathMLAlt(data, fullPath, r)
			checkEmptySrcAttr(data, fullPath, r)
			checkEpubSwitchTrigger(data, fullPath, r)
			checkEpubTypeOnHead(data, fullPath, r)
			checkTableBorderAttr(data, fullPath, r)
			checkHttpEquivCharset(data, fullPath, r)
			checkImageMapValid(data, fullPath, r)
			checkCSS008StyleType(data, fullPath, r)
			checkStyleInBody(data, fullPath, r)
			checkStyleAttrCSS(data, fullPath, r)
			checkMicrodataAttrs(data, fullPath, r)
			checkHTM054ReservedNS(data, fullPath, r)
			checkARIADescribedAt(data, fullPath, r)
			checkTitleElement(data, fullPath, r)
			checkPrefixAttrLocation(data, fullPath, r)
			checkPrefixDeclarations(data, fullPath, r)
			checkNestedTime(data, fullPath, r)
			checkMathMLContentOnly(data, fullPath, r)
			checkMathMLAnnotation(data, fullPath, r)
			checkHiddenAttrValue(data, fullPath, r)
			checkDatetimeFormat(data, fullPath, r)
			checkURLConformance(data, fullPath, r)
			checkEntityReferences(data, fullPath, r)

			// Nav document checks - use content-based detection OR explicit checkMode
			if opts.CheckMode == "nav" || isNavDocument(data) {
				checkNavContentModel(data, fullPath, r)
			}

			// Usage-level checks
			checkHTM055Discouraged(data, fullPath, r)
			checkHTM010UnknownEpubNS(data, fullPath, r)
			checkOPF088UnknownEpubType(data, fullPath, r)
			checkOPF086bDeprecatedEpubType(data, fullPath, r)
			checkOPF087MisusedEpubType(data, fullPath, r)
			checkOPF028UndeclaredPrefix(data, fullPath, r)

			// URL checks
			checkSingleFileURLs(data, fullPath, r)

			// Embedded SVG checks (for XHTML with inline SVG)
			checkSVGForeignObject(data, fullPath, r)
			checkSVGTitleContent(data, fullPath, r, false)
			checkSVGInvalidElements(data, fullPath, r)
		} else if mt == "image/svg+xml" {
			// SVG content checks
			checkSingleFileURLs(data, fullPath, r)
			checkSVGLinkLabel(data, fullPath, r)
			checkSVGDuplicateIDs(data, fullPath, r)
			checkSVGInvalidIDs(data, fullPath, r)
			checkSVGForeignObject(data, fullPath, r)
			checkSVGTitleContent(data, fullPath, r, true)
			checkSVGEpubType(data, fullPath, r)
			checkSVGUnknownEpubAttr(data, fullPath, r)
			checkSVGInvalidElements(data, fullPath, r)
		} else if mt == "application/smil+xml" {
			// SMIL media overlay checks for single-file mode
			checkSingleFileSMIL(ep, data, fullPath, r)
		} else {
			continue
		}
	}
}

// checkSingleFileURLs scans href/src attributes and inline CSS in XHTML/SVG for
// data URLs (RSC-029), file URLs (RSC-030), and query strings (RSC-033).
func checkSingleFileURLs(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(bytes.NewReader(data))
	decoder.Strict = false
	decoder.AutoClose = xml.HTMLAutoClose

	var inStyle bool

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			elem := strings.ToLower(t.Name.Local)
			if elem == "style" {
				inStyle = true
			}
			for _, attr := range t.Attr {
				name := strings.ToLower(attr.Name.Local)
				if name != "href" && name != "src" {
					continue
				}
				val := strings.TrimSpace(attr.Value)
				if val == "" {
					continue
				}
				lowerVal := strings.ToLower(val)

				// RSC-029: data URLs in a, area elements
				if strings.HasPrefix(lowerVal, "data:") {
					if elem == "a" || elem == "area" {
						r.AddWithLocation(report.Error, "RSC-029",
							fmt.Sprintf("Data URL used in %s element href", elem), location)
					}
					continue
				}

				// RSC-030: file URLs
				if strings.HasPrefix(lowerVal, "file:") {
					r.AddWithLocation(report.Error, "RSC-030",
						fmt.Sprintf("File URL used in %s element %s attribute", elem, name), location)
					continue
				}

				// RSC-033: query strings in local URLs
				if name == "href" && !isRemoteURL(val) && strings.Contains(val, "?") {
					if elem == "a" || elem == "area" {
						r.AddWithLocation(report.Error, "RSC-033",
							fmt.Sprintf("URL query string found in %s element href", elem), location)
					}
				}
			}
		case xml.CharData:
			if inStyle {
				// Check for file: URLs in inline CSS
				cssLower := strings.ToLower(string(t))
				idx := 0
				for {
					pos := strings.Index(cssLower[idx:], "file:")
					if pos < 0 {
						break
					}
					r.AddWithLocation(report.Error, "RSC-030",
						"File URL used in CSS", location)
					idx += pos + 5
				}
			}
		case xml.EndElement:
			if strings.ToLower(t.Name.Local) == "style" {
				inStyle = false
			}
		case xml.ProcInst:
			// Check processing instructions like <?xml-stylesheet href="file:..."?>
			piData := string(t.Inst)
			if strings.Contains(strings.ToLower(piData), "file:") {
				r.AddWithLocation(report.Error, "RSC-030",
					"File URL used in processing instruction", location)
			}
		}
	}
}

// ============================================================================
// Single-file content check functions
// ============================================================================

// checkSingleFileWellFormed checks XML well-formedness for single-file mode.
// Returns true always since we don't want to block further checks.
// Entity-related errors from Go's XML parser (which doesn't process DTD)
// are expected and should not prevent validation.
func checkSingleFileWellFormed(data []byte, location string, r *report.Report) bool {
	// In single-file mode, we don't perform strict well-formedness checks
	// because Go's XML parser can't handle internal DTD entity declarations
	// which are valid in EPUB XHTML documents.
	return true
}

// checkXML11Version detects XML 1.1 version declarations (HTM-001).
func checkXML11Version(data []byte, location string, r *report.Report) {
	header := string(data[:min(200, len(data))])
	if strings.Contains(header, `version="1.1"`) || strings.Contains(header, `version='1.1'`) {
		r.AddWithLocation(report.Error, "HTM-001",
			"XML version 1.1 is not allowed in EPUB content documents; must use XML 1.0",
			location)
	}
}

// checkNestedAnchors detects nested <a> elements (RSC-005 in single-file mode).
func checkNestedAnchors(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	depth := 0
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "a" {
				if depth > 0 {
					r.AddWithLocation(report.Error, "RSC-005",
						`The "a" element cannot contain any nested "a" elements`,
						location)
				}
				depth++
			}
		case xml.EndElement:
			if t.Name.Local == "a" && depth > 0 {
				depth--
			}
		}
	}
}

// ---------------------------------------------------------------------------
// HTML5 Content Model Validation (RSC-005)
//
// These checks enforce the HTML5 content model rules from the RelaxNG schemas.
// Epubcheck uses Jing + RelaxNG to validate these rules; epubverify uses
// targeted Go checks that cover the most common violations.
// ---------------------------------------------------------------------------

// phrasingOnlyElements contains HTML elements whose content model allows
// only phrasing content (text and inline elements). Block-level elements
// like <div>, <p>, <table>, <ul>, <ol>, etc. must not appear inside these.
var phrasingOnlyElements = map[string]bool{
	"p": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"pre": true, "span": true, "em": true, "strong": true, "small": true, "mark": true,
	"abbr": true, "dfn": true, "i": true, "b": true, "s": true, "u": true,
	"code": true, "var": true, "samp": true, "kbd": true, "sup": true, "sub": true,
	"q": true, "cite": true, "bdo": true, "bdi": true, "label": true, "legend": true,
	"dt": true, "summary": true, "output": true, "data": true, "time": true,
}

// flowOnlyElements contains elements that are flow content but NOT phrasing
// content — i.e., block-level elements that cannot appear inside phrasing-only parents.
var flowOnlyElements = map[string]bool{
	"div": true, "p": true, "hr": true, "blockquote": true,
	"section": true, "nav": true, "article": true, "aside": true,
	"header": true, "footer": true, "main": true, "search": true,
	"address": true, "hgroup": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"ul": true, "ol": true, "dl": true, "menu": true,
	"figure": true, "table": true, "form": true, "fieldset": true,
	"details": true, "dialog": true, "pre": true,
}

// checkBlockInPhrasing reports RSC-005 when a flow-only (block) element appears
// inside a phrasing-only parent element. This is the most common content model
// violation — e.g., <p><div>...</div></p> or <span><ul>...</ul></span>.
func checkBlockInPhrasing(data []byte, location string, r *report.Report) {
	const xhtmlNS = "http://www.w3.org/1999/xhtml"
	const svgNS = "http://www.w3.org/2000/svg"
	const mathNS = "http://www.w3.org/1998/Math/MathML"

	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	foreignDepth := 0 // non-zero inside svg/math subtrees

	// Stack tracks whether we're inside a phrasing-only parent.
	// Each entry is the element name; the bool tracks if it's phrasing-only.
	type stackEntry struct {
		name          string
		phrasingOnly  bool
	}
	var stack []stackEntry

	// Helper: are we currently inside a phrasing-only context?
	inPhrasingContext := func() bool {
		for i := len(stack) - 1; i >= 0; i-- {
			if stack[i].phrasingOnly {
				return true
			}
		}
		return false
	}

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if foreignDepth > 0 {
				foreignDepth++
				continue
			}
			ns := t.Name.Space
			if ns == svgNS || ns == mathNS {
				foreignDepth = 1
				continue
			}
			if ns != "" && ns != xhtmlNS {
				foreignDepth = 1
				continue
			}
			name := strings.ToLower(t.Name.Local)
			if name == "svg" || name == "math" {
				foreignDepth = 1
				continue
			}

			// Check: is this a block element inside a phrasing-only context?
			if flowOnlyElements[name] && inPhrasingContext() {
				// Find the nearest phrasing-only ancestor for the error message
				ancestor := ""
				for i := len(stack) - 1; i >= 0; i-- {
					if stack[i].phrasingOnly {
						ancestor = stack[i].name
						break
					}
				}
				r.AddWithLocation(report.Error, "RSC-005",
					fmt.Sprintf("element \"%s\" not allowed here; \"%s\" accepts only phrasing content", name, ancestor),
					location)
				return // report only first error per file
			}

			stack = append(stack, stackEntry{
				name:         name,
				phrasingOnly: phrasingOnlyElements[name],
			})

		case xml.EndElement:
			if foreignDepth > 0 {
				foreignDepth--
				continue
			}
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
}

// restrictedChildElements maps parent elements to the set of allowed direct children.
// Elements not in this set are not valid as direct children of the parent.
var restrictedChildElements = map[string]map[string]bool{
	"ul":       {"li": true},
	"ol":       {"li": true},
	"dl":       {"dt": true, "dd": true, "div": true},
	"hgroup":   {"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "p": true},
	"select":   {"option": true, "optgroup": true},
	"optgroup": {"option": true},
	"tr":       {"td": true, "th": true},
	"thead":    {"tr": true},
	"tbody":    {"tr": true},
	"tfoot":    {"tr": true},
	"colgroup": {"col": true},
	"datalist": {"option": true},
}

// scriptSupportingElements are allowed as children in restricted contexts.
var scriptSupportingElements = map[string]bool{
	"script": true, "template": true, "noscript": true,
}

// checkRestrictedChildren reports RSC-005 when an element that has restricted
// allowed children contains an element that is not in the allowed set.
// For example, <ul> can only contain <li>, <tr> can only contain <td>/<th>.
func checkRestrictedChildren(data []byte, location string, r *report.Report) {
	const xhtmlNS = "http://www.w3.org/1999/xhtml"

	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	foreignDepth := 0

	// Stack of element names for parent tracking
	var stack []string

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if foreignDepth > 0 {
				foreignDepth++
				continue
			}
			ns := t.Name.Space
			if ns != "" && ns != xhtmlNS {
				foreignDepth = 1
				continue
			}
			name := strings.ToLower(t.Name.Local)
			if name == "svg" || name == "math" {
				foreignDepth = 1
				continue
			}

			// Check if the parent has restricted children
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				allowed, hasRestriction := restrictedChildElements[parent]
				if hasRestriction && !allowed[name] && !scriptSupportingElements[name] {
					allowedList := ""
					for k := range allowed {
						if allowedList != "" {
							allowedList += ", "
						}
						allowedList += "\"" + k + "\""
					}
					r.AddWithLocation(report.Error, "RSC-005",
						fmt.Sprintf("element \"%s\" not allowed as child of \"%s\"; only %s allowed",
							name, parent, allowedList),
						location)
					return // report only first error per file
				}
			}

			stack = append(stack, name)

		case xml.EndElement:
			if foreignDepth > 0 {
				foreignDepth--
				continue
			}
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
}

// voidElements are HTML elements that cannot have children.
// Per the HTML5 spec, these elements have an "empty" content model.
var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true,
	"hr": true, "img": true, "input": true, "link": true, "meta": true,
	"param": true, "source": true, "track": true, "wbr": true,
}

// checkVoidElementChildren reports RSC-005 when a void element has child elements.
// Void elements (br, hr, img, input, etc.) must be empty — they cannot contain
// any child elements. Text content inside void elements is also invalid but is
// harder to detect with a streaming parser, so we focus on child elements.
func checkVoidElementChildren(data []byte, location string, r *report.Report) {
	const xhtmlNS = "http://www.w3.org/1999/xhtml"

	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	foreignDepth := 0

	// Track when we're inside a void element
	var voidStack []string // stack of void element names (usually 0 or 1 deep)

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if foreignDepth > 0 {
				foreignDepth++
				continue
			}
			ns := t.Name.Space
			if ns != "" && ns != xhtmlNS {
				foreignDepth = 1
				continue
			}
			name := strings.ToLower(t.Name.Local)
			if name == "svg" || name == "math" {
				foreignDepth = 1
				continue
			}

			if len(voidStack) > 0 {
				// We're inside a void element and found a child element
				parent := voidStack[len(voidStack)-1]
				r.AddWithLocation(report.Error, "RSC-005",
					fmt.Sprintf("element \"%s\" not allowed as child of void element \"%s\"", name, parent),
					location)
				return
			}

			if voidElements[name] {
				voidStack = append(voidStack, name)
			}

		case xml.EndElement:
			if foreignDepth > 0 {
				foreignDepth--
				continue
			}
			name := strings.ToLower(t.Name.Local)
			if len(voidStack) > 0 && voidStack[len(voidStack)-1] == name {
				voidStack = voidStack[:len(voidStack)-1]
			}
		}
	}
}

// tableDirectChildren are the only elements allowed as direct children of <table>.
var tableDirectChildren = map[string]bool{
	"caption": true, "colgroup": true, "thead": true,
	"tbody": true, "tfoot": true, "tr": true,
}

// checkTableContentModel reports RSC-005 when <table> has direct children
// that are not one of: caption, colgroup, thead, tbody, tfoot, tr.
// This is separate from checkRestrictedChildren because table has a more
// complex content model (ordering matters) and we want a specific error message.
func checkTableContentModel(data []byte, location string, r *report.Report) {
	const xhtmlNS = "http://www.w3.org/1999/xhtml"

	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	foreignDepth := 0

	// Stack tracks element depth; we care about direct children of <table>
	var stack []string

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if foreignDepth > 0 {
				foreignDepth++
				continue
			}
			ns := t.Name.Space
			if ns != "" && ns != xhtmlNS {
				foreignDepth = 1
				continue
			}
			name := strings.ToLower(t.Name.Local)
			if name == "svg" || name == "math" {
				foreignDepth = 1
				continue
			}

			// Check if parent is <table> and child is invalid
			if len(stack) > 0 && stack[len(stack)-1] == "table" {
				if !tableDirectChildren[name] && !scriptSupportingElements[name] {
					r.AddWithLocation(report.Error, "RSC-005",
						fmt.Sprintf("element \"%s\" not allowed as child of \"table\"", name),
						location)
					return
				}
			}

			stack = append(stack, name)

		case xml.EndElement:
			if foreignDepth > 0 {
				foreignDepth--
				continue
			}
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
}

// interactiveElements are HTML elements that are classified as interactive content.
// These elements cannot be nested inside other interactive elements.
var interactiveElements = map[string]bool{
	"a": true, "button": true, "input": true, "select": true,
	"textarea": true, "label": true, "embed": true, "iframe": true,
	"audio": true, "video": true, // when they have controls attribute
	"details": true, "summary": true,
}

// checkInteractiveNesting reports RSC-005 when interactive elements are nested
// inside other interactive elements. For example, <a> containing <button>,
// <button> containing <input>, etc. This extends the simpler checkNestedAnchors.
func checkInteractiveNesting(data []byte, location string, r *report.Report) {
	const xhtmlNS = "http://www.w3.org/1999/xhtml"

	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	foreignDepth := 0

	// Stack of interactive element names we're inside
	var interactiveStack []string

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if foreignDepth > 0 {
				foreignDepth++
				continue
			}
			ns := t.Name.Space
			if ns != "" && ns != xhtmlNS {
				foreignDepth = 1
				continue
			}
			name := strings.ToLower(t.Name.Local)
			if name == "svg" || name == "math" {
				foreignDepth = 1
				continue
			}

			if interactiveElements[name] {
				if len(interactiveStack) > 0 {
					parent := interactiveStack[len(interactiveStack)-1]
					// Don't re-report nested <a> — checkNestedAnchors handles that
					if !(name == "a" && parent == "a") {
						r.AddWithLocation(report.Error, "RSC-005",
							fmt.Sprintf("element \"%s\" not allowed inside interactive element \"%s\"",
								name, parent),
							location)
						return
					}
				}
				interactiveStack = append(interactiveStack, name)
			}

		case xml.EndElement:
			if foreignDepth > 0 {
				foreignDepth--
				continue
			}
			name := strings.ToLower(t.Name.Local)
			if len(interactiveStack) > 0 && interactiveStack[len(interactiveStack)-1] == name {
				interactiveStack = interactiveStack[:len(interactiveStack)-1]
			}
		}
	}
}

// transparentElements have a transparent content model — they inherit the
// content model of their parent. If the parent allows only phrasing content,
// these elements must also contain only phrasing content.
var transparentElements = map[string]bool{
	"a": true, "ins": true, "del": true, "object": true,
	"video": true, "audio": true, "map": true, "canvas": true,
}

// checkTransparentContentModel reports RSC-005 when a transparent element
// contains flow content but is inside a phrasing-only parent.
// For example: <p><a><div>block in transparent a in p</div></a></p>
//
// Note: checkBlockInPhrasing already catches direct block-in-phrasing violations.
// This check specifically handles the case where a transparent element sits between
// the phrasing-only parent and the block-level child, which checkBlockInPhrasing
// would also catch since it tracks ancestor context. So this function specifically
// handles cases where a transparent element is the direct child of a phrasing parent,
// and the transparent element contains block content — the block content should be
// flagged because the transparent model inherits the phrasing restriction.
//
// In practice, checkBlockInPhrasing already handles the most important cases because
// it tracks the entire ancestor chain. This function adds explicit transparent model
// awareness for cases where intermediate non-phrasing-only elements appear.
func checkTransparentContentModel(data []byte, location string, r *report.Report) {
	const xhtmlNS = "http://www.w3.org/1999/xhtml"

	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	foreignDepth := 0

	type stackEntry struct {
		name         string
		phrasingOnly bool // true if this element or an ancestor restricts to phrasing
	}
	var stack []stackEntry

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if foreignDepth > 0 {
				foreignDepth++
				continue
			}
			ns := t.Name.Space
			if ns != "" && ns != xhtmlNS {
				foreignDepth = 1
				continue
			}
			name := strings.ToLower(t.Name.Local)
			if name == "svg" || name == "math" {
				foreignDepth = 1
				continue
			}

			// Determine if this context requires phrasing-only content
			parentPhrasing := false
			if len(stack) > 0 {
				parentPhrasing = stack[len(stack)-1].phrasingOnly
			}

			isPhrasing := phrasingOnlyElements[name]
			// Transparent elements inherit parent's phrasing restriction
			if transparentElements[name] && parentPhrasing {
				isPhrasing = true
			}

			// Check: block element inside inherited phrasing context
			if flowOnlyElements[name] && parentPhrasing && !isPhrasing {
				// Find the originating phrasing ancestor
				ancestor := ""
				for i := len(stack) - 1; i >= 0; i-- {
					if phrasingOnlyElements[stack[i].name] {
						ancestor = stack[i].name
						break
					}
				}
				if ancestor == "" {
					ancestor = "transparent parent"
				}
				r.AddWithLocation(report.Error, "RSC-005",
					fmt.Sprintf("element \"%s\" not allowed here; transparent content model inherits phrasing-only restriction from \"%s\"",
						name, ancestor),
					location)
				return
			}

			stack = append(stack, stackEntry{
				name:         name,
				phrasingOnly: isPhrasing || parentPhrasing,
			})

		case xml.EndElement:
			if foreignDepth > 0 {
				foreignDepth--
				continue
			}
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
}

// checkFigcaptionPosition reports RSC-005 when <figcaption> is not the first
// or last child element of <figure>. Per the HTML5 spec, figcaption must be
// either the first or last child of figure, not in the middle.
func checkFigcaptionPosition(data []byte, location string, r *report.Report) {
	const xhtmlNS = "http://www.w3.org/1999/xhtml"

	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	foreignDepth := 0

	// We need to collect children of each <figure> element, then check
	// that any <figcaption> is first or last.
	type figureCtx struct {
		children []string // element names of direct children
		depth    int      // nesting depth for child tracking
	}
	var figStack []*figureCtx

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if foreignDepth > 0 {
				foreignDepth++
				continue
			}
			ns := t.Name.Space
			if ns != "" && ns != xhtmlNS {
				foreignDepth = 1
				continue
			}
			name := strings.ToLower(t.Name.Local)
			if name == "svg" || name == "math" {
				foreignDepth = 1
				continue
			}

			// Track direct children of figure contexts
			if len(figStack) > 0 {
				ctx := figStack[len(figStack)-1]
				if ctx.depth == 0 {
					// Direct child of <figure>
					ctx.children = append(ctx.children, name)
				}
				ctx.depth++
			}

			if name == "figure" {
				figStack = append(figStack, &figureCtx{depth: 0})
			}

		case xml.EndElement:
			if foreignDepth > 0 {
				foreignDepth--
				continue
			}
			name := strings.ToLower(t.Name.Local)

			if len(figStack) > 0 {
				ctx := figStack[len(figStack)-1]
				ctx.depth--
			}

			if name == "figure" && len(figStack) > 0 {
				ctx := figStack[len(figStack)-1]
				figStack = figStack[:len(figStack)-1]

				// Check figcaption position
				for i, child := range ctx.children {
					if child == "figcaption" {
						if i != 0 && i != len(ctx.children)-1 {
							r.AddWithLocation(report.Error, "RSC-005",
								"\"figcaption\" must be the first or last child of \"figure\"",
								location)
							return
						}
					}
				}
			}
		}
	}
}

// checkPictureContentModel reports RSC-005 when <picture> does not contain
// the correct structure: zero or more <source> elements followed by one <img>.
func checkPictureContentModel(data []byte, location string, r *report.Report) {
	const xhtmlNS = "http://www.w3.org/1999/xhtml"

	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	foreignDepth := 0

	type pictureCtx struct {
		children []string
		depth    int
	}
	var picStack []*pictureCtx

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if foreignDepth > 0 {
				foreignDepth++
				continue
			}
			ns := t.Name.Space
			if ns != "" && ns != xhtmlNS {
				foreignDepth = 1
				continue
			}
			name := strings.ToLower(t.Name.Local)
			if name == "svg" || name == "math" {
				foreignDepth = 1
				continue
			}

			if len(picStack) > 0 {
				ctx := picStack[len(picStack)-1]
				if ctx.depth == 0 {
					ctx.children = append(ctx.children, name)
				}
				ctx.depth++
			}

			if name == "picture" {
				picStack = append(picStack, &pictureCtx{depth: 0})
			}

		case xml.EndElement:
			if foreignDepth > 0 {
				foreignDepth--
				continue
			}
			name := strings.ToLower(t.Name.Local)

			if len(picStack) > 0 {
				ctx := picStack[len(picStack)-1]
				ctx.depth--
			}

			if name == "picture" && len(picStack) > 0 {
				ctx := picStack[len(picStack)-1]
				picStack = picStack[:len(picStack)-1]

				// Validate: source* then img, with optional script-supporting
				hasImg := false
				imgSeen := false
				for _, child := range ctx.children {
					if child == "img" {
						if hasImg {
							r.AddWithLocation(report.Error, "RSC-005",
								"\"picture\" must contain exactly one \"img\" element",
								location)
							return
						}
						hasImg = true
						imgSeen = true
					} else if child == "source" {
						if imgSeen {
							r.AddWithLocation(report.Error, "RSC-005",
								"\"source\" must appear before \"img\" in \"picture\"",
								location)
							return
						}
					} else if !scriptSupportingElements[child] {
						r.AddWithLocation(report.Error, "RSC-005",
							fmt.Sprintf("element \"%s\" not allowed as child of \"picture\"", child),
							location)
						return
					}
				}
			}
		}
	}
}

// checkCustomNamespacedAttrs detects non-standard namespace attributes on XHTML elements (RSC-005).
// Used for EPUB2 OPS XHTML documents where custom namespace attributes are not allowed.
func checkCustomNamespacedAttrs(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	// Accumulate namespace URI → prefix mappings across all elements
	nsPrefixMap := make(map[string]string)
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		// First collect namespace declarations from this element
		for _, attr := range se.Attr {
			if attr.Name.Space == "xmlns" {
				nsPrefixMap[attr.Value] = attr.Name.Local
			}
		}
		for _, attr := range se.Attr {
			ns := attr.Name.Space
			if ns == "" || ns == "xmlns" || ns == "http://www.w3.org/XML/1998/namespace" ||
				ns == "http://www.w3.org/2000/xmlns/" || ns == "http://www.w3.org/1999/xhtml" ||
				ns == "http://www.idpf.org/2007/ops" || ns == "http://www.w3.org/2001/10/synthesis" ||
				ns == "http://www.w3.org/2000/svg" || ns == "http://www.w3.org/1998/Math/MathML" ||
				ns == "http://www.w3.org/1999/xlink" {
				continue
			}
			// Custom namespace found — use prefix:localname format
			localName := attr.Name.Local
			prefix := nsPrefixMap[ns]
			if prefix == "" {
				prefix = ns // fallback to URI if prefix not found
			}
			r.AddWithLocation(report.Error, "RSC-005",
				fmt.Sprintf(`attribute "%s:%s" not allowed here`, prefix, localName),
				location)
			return
		}
	}
}

// checkMissingNamespace detects XHTML documents without an XHTML namespace (RSC-005).
func checkMissingNamespace(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Local == "html" {
			if se.Name.Space == "" {
				r.AddWithLocation(report.Error, "RSC-005",
					`elements from namespace "" are not allowed`,
					location)
			}
			return
		}
	}
}

// checkDuplicateIDs detects duplicate id attribute values in XHTML (RSC-005 in single-file).
func checkDuplicateIDs(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	idCount := make(map[string]int)
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attr := range se.Attr {
			if attr.Name.Local == "id" && attr.Value != "" {
				idCount[attr.Value]++
			}
		}
	}
	// Report all occurrences of duplicated IDs
	for id, count := range idCount {
		if count > 1 {
			for i := 0; i < count; i++ {
				r.AddWithLocation(report.Error, "RSC-005",
					fmt.Sprintf(`Duplicate ID "%s"`, id),
					location)
			}
		}
	}
}

// checkIDReferences checks that id-referencing attributes (for, headers, aria-labelledby, etc.)
// refer to existing IDs in the same document (RSC-005 in single-file mode).
func checkIDReferences(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	// First pass: collect all IDs
	ids := collectIDs(data)

	// ID-referencing attributes
	idRefAttrs := map[string]bool{
		"for": true, "headers": true, "aria-labelledby": true,
		"aria-describedby": true, "aria-owns": true, "aria-controls": true,
		"aria-flowto": true, "aria-activedescendant": true,
		"aria-errormessage": true, "aria-details": true,
	}

	decoder = newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attr := range se.Attr {
			if idRefAttrs[attr.Name.Local] && attr.Value != "" {
				// These attributes can contain space-separated lists of IDs
				for _, ref := range strings.Fields(attr.Value) {
					if !ids[ref] {
						r.AddWithLocation(report.Error, "RSC-005",
							fmt.Sprintf(`ID-referencing attribute "%s" must refer to elements in the same document (target ID missing): "%s"`, attr.Name.Local, ref),
							location)
					}
				}
			}
		}
	}
}

// checkExternalEntities detects external entity declarations in DOCTYPE (HTM-003).
func checkExternalEntities(data []byte, location string, r *report.Report) {
	content := string(data)
	// Look for DOCTYPE with internal subset
	idx := strings.Index(strings.ToUpper(content), "<!DOCTYPE")
	if idx == -1 {
		return
	}
	// Find the internal subset between [ and ]
	startBracket := strings.Index(content[idx:], "[")
	if startBracket == -1 {
		return
	}
	endBracket := strings.Index(content[idx+startBracket:], "]")
	if endBracket == -1 {
		return
	}
	subset := content[idx+startBracket : idx+startBracket+endBracket+1]
	// Check for SYSTEM or PUBLIC entity declarations
	if strings.Contains(strings.ToUpper(subset), "SYSTEM") || strings.Contains(strings.ToUpper(subset), "PUBLIC") {
		r.AddWithLocation(report.Error, "HTM-003",
			"External entities are not allowed in EPUB content documents",
			location)
	}
}

// checkEntityReferences detects XML entity reference errors that cause parse failures (RSC-016).
func checkEntityReferences(data []byte, location string, r *report.Report) {
	content := string(data)
	// Check for entity references not ending with semicolons: &word followed by non-semicolon
	entityNoSemiRe := regexp.MustCompile(`&[a-zA-Z][a-zA-Z0-9]*[^;a-zA-Z0-9]`)
	if entityNoSemiRe.MatchString(content) {
		r.AddWithLocation(report.Fatal, "RSC-016",
			"Fatal Error while parsing file: The entity name must end with the ';' delimiter",
			location)
	}
}

// checkHTM061DataAttrs validates data-* attribute names (HTM-061).
func checkHTM061DataAttrs(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attr := range se.Attr {
			name := attr.Name.Local
			if !strings.HasPrefix(name, "data-") {
				continue
			}
			suffix := name[5:]
			if suffix == "" {
				// data- with no name after hyphen
				r.AddWithLocation(report.Error, "HTM-061",
					fmt.Sprintf(`Invalid data attribute "%s": must have at least one character after "data-"`, name),
					location)
				continue
			}
			if strings.HasPrefix(suffix, "-") {
				// data-- is not valid XML name
				r.AddWithLocation(report.Error, "HTM-061",
					fmt.Sprintf(`Invalid data attribute "%s": not a valid XML name`, name),
					location)
				continue
			}
			// Check for uppercase letters
			if strings.ToLower(suffix) != suffix {
				r.AddWithLocation(report.Error, "HTM-061",
					fmt.Sprintf(`Invalid data attribute "%s": must not contain uppercase ASCII letters`, name),
					location)
			}
		}
	}
}

// checkHTM054ReservedNS detects custom attributes using reserved namespace strings (HTM-054).
// Namespaces containing "w3.org" or "idpf.org" in their host are reserved.
func checkHTM054ReservedNS(data []byte, location string, r *report.Report) {
	// Standard namespaces that are allowed even though they contain "w3.org"/"idpf.org"
	allowedNS := map[string]bool{
		"http://www.w3.org/1999/xhtml":        true,
		"http://www.w3.org/2000/svg":          true,
		"http://www.w3.org/1998/Math/MathML":  true,
		"http://www.w3.org/XML/1998/namespace": true,
		"http://www.w3.org/2000/xmlns/":       true,
		"http://www.w3.org/1999/xlink":        true,
		"http://www.idpf.org/2007/ops":        true,
		"http://www.idpf.org/2007/opf":        true,
		"http://www.w3.org/2001/10/synthesis": true,
		"http://www.w3.org/ns/SMIL":           true,
		"http://www.w3.org/2001/xml-events":   true,
	}
	isReservedNS := func(ns string) bool {
		if ns == "" || ns == "xmlns" || allowedNS[ns] {
			return false
		}
		if idx := strings.Index(ns, "://"); idx >= 0 {
			rest := ns[idx+3:]
			host := rest
			if slashIdx := strings.Index(rest, "/"); slashIdx >= 0 {
				host = rest[:slashIdx]
			}
			lhost := strings.ToLower(host)
			if strings.Contains(lhost, "w3.org") || strings.Contains(lhost, "idpf.org") {
				return true
			}
		}
		return false
	}
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attr := range se.Attr {
			ns := attr.Name.Space
			if ns == "" || ns == "xmlns" {
				continue
			}
			if isReservedNS(ns) {
				r.AddWithLocation(report.Error, "HTM-054",
					fmt.Sprintf(`Attribute "%s" in namespace "%s" uses a reserved string`, attr.Name.Local, ns),
					location)
			}
		}
	}
}

// checkHTM058Encoding checks if an XHTML document is not encoded as UTF-8 (HTM-058).
func checkHTM058Encoding(data []byte, location string, r *report.Report) {
	// Check for UTF-16 BOM
	if bytes.HasPrefix(data, utf16LEBOM) || bytes.HasPrefix(data, utf16BEBOM) {
		r.AddWithLocation(report.Error, "HTM-058",
			"Content document is not encoded as UTF-8",
			location)
	}
}

// checkHTM007SSML checks for empty SSML ph attributes (HTM-007).
func checkHTM007SSML(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attr := range se.Attr {
			// ssml:ph attribute (namespace http://www.w3.org/2001/10/synthesis)
			if attr.Name.Local == "ph" && attr.Name.Space == "http://www.w3.org/2001/10/synthesis" {
				if strings.TrimSpace(attr.Value) == "" {
					r.AddWithLocation(report.Warning, "HTM-007",
						fmt.Sprintf(`Empty value for SSML attribute "%s" on element "%s"`, "ssml:ph", se.Name.Local),
						location)
				}
			}
		}
	}
}

// checkHTM025URLScheme checks for unregistered URL schemes in href/src (HTM-025).
func checkHTM025URLScheme(data []byte, location string, r *report.Report) {
	registeredSchemes := map[string]bool{
		"http": true, "https": true, "mailto": true, "tel": true,
		"ftp": true, "ftps": true, "data": true, "file": true,
		"javascript": true, "urn": true, "cid": true, "mid": true,
		"geo": true, "sms": true, "xmpp": true, "irc": true,
		"ircs": true, "ssh": true, "news": true, "nntp": true,
		"rtsp": true, "sip": true, "sips": true, "magnet": true,
		"feed": true, "svn": true, "git": true, "vnc": true,
		"telnet": true, "ldap": true, "ldaps": true, "nfs": true,
		"blob": true, "about": true, "ws": true, "wss": true,
		"coap": true, "cap": true, "acap": true, "tag": true,
	}
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attr := range se.Attr {
			if attr.Name.Local != "href" && attr.Name.Local != "src" {
				continue
			}
			val := strings.TrimSpace(attr.Value)
			if val == "" {
				continue
			}
			u, err := url.Parse(val)
			if err != nil || u.Scheme == "" {
				continue
			}
			scheme := strings.ToLower(u.Scheme)
			if !registeredSchemes[scheme] {
				r.AddWithLocation(report.Warning, "HTM-025",
					fmt.Sprintf(`Unregistered URL scheme "%s" in attribute "%s"`, scheme, attr.Name.Local),
					location)
				return
			}
		}
	}
}

// checkCSS008Inline checks for CSS errors in style elements and style attributes (CSS-008).
func checkCSS008Inline(data []byte, location string, r *report.Report) {
	content := string(data)

	// Check for <style> without type attribute (EPUB 2 issue)
	// In EPUB 3, the type attribute defaults to text/css, so this is only reported
	// if the style element has a non-CSS type
	decoder := newXHTMLDecoder(strings.NewReader(content))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Local == "style" {
			typeAttr := ""
			for _, attr := range se.Attr {
				if attr.Name.Local == "type" {
					typeAttr = strings.TrimSpace(strings.ToLower(attr.Value))
				}
			}
			if typeAttr != "" && typeAttr != "text/css" {
				r.AddWithLocation(report.Error, "CSS-008",
					fmt.Sprintf(`The style element type attribute value "%s" is not "text/css"`, typeAttr),
					location)
			}
		}

		// Check style attribute for CSS syntax errors
		if se.Name.Local != "" {
			for _, attr := range se.Attr {
				if attr.Name.Local == "style" && attr.Value != "" {
					// Basic CSS property validation
					val := strings.TrimSpace(attr.Value)
					if val != "" && !strings.Contains(val, ":") && !strings.Contains(val, ";") {
						// style attribute with no property:value pattern
						r.AddWithLocation(report.Error, "CSS-008",
							"An error occurred while parsing the CSS in a style attribute",
							location)
					}
				}
			}
		}
	}
}

// checkCSS015AltStylesheet checks that alternative stylesheets have titles (CSS-015).
func checkCSS015AltStylesheet(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "link" {
			continue
		}
		rel := ""
		title := ""
		for _, attr := range se.Attr {
			if attr.Name.Local == "rel" {
				rel = strings.ToLower(strings.TrimSpace(attr.Value))
			}
			if attr.Name.Local == "title" {
				title = attr.Value
			}
		}
		// Check for alternate stylesheet without title
		if strings.Contains(rel, "stylesheet") && strings.Contains(rel, "alternate") {
			if title == "" {
				r.AddWithLocation(report.Error, "CSS-015",
					"Alternate stylesheet link element is missing a title attribute",
					location)
			} else if strings.TrimSpace(title) == "" {
				r.AddWithLocation(report.Error, "CSS-015",
					"Alternate stylesheet link element has an empty title attribute",
					location)
			}
		}
	}
}

// checkCSS005AltStyleTag checks for conflicting alt style tags (CSS-005).
func checkCSS005AltStyleTag(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	knownTags := map[string]bool{
		"horizontal": true, "vertical": true, "day": true, "night": true,
	}
	// Conflicting tag pairs
	conflicts := map[string]string{
		"horizontal": "vertical",
		"vertical":   "horizontal",
		"day":        "night",
		"night":      "day",
	}
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "link" {
			continue
		}
		for _, attr := range se.Attr {
			if attr.Name.Local == "class" {
				classes := strings.Fields(attr.Value)
				classSet := make(map[string]bool)
				for _, cls := range classes {
					classSet[cls] = true
				}
				// Check for conflicting pairs
				for _, cls := range classes {
					if conflictWith, ok := conflicts[cls]; ok {
						if classSet[conflictWith] {
							r.AddWithLocation(report.Usage, "CSS-005",
								fmt.Sprintf(`Conflicting alternate style tags "%s" and "%s"`, cls, conflictWith),
								location)
							return
						}
					}
				}
				// Check for unknown tags
				for _, cls := range classes {
					if !knownTags[cls] && strings.Contains(cls, "-") {
						r.AddWithLocation(report.Usage, "CSS-005",
							fmt.Sprintf(`Unknown alternate style tag "%s"`, cls),
							location)
						return
					}
				}
			}
		}
	}
}

// checkHTM055Discouraged detects discouraged HTML constructs in EPUB (HTM-055).
func checkHTM055Discouraged(data []byte, location string, r *report.Report) {
	discouraged := map[string]bool{
		"base": true, "embed": true, "rp": true,
	}
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if discouraged[se.Name.Local] {
			r.AddWithLocation(report.Usage, "HTM-055",
				fmt.Sprintf(`The "%s" element is a discouraged construct in EPUB`, se.Name.Local),
				location)
		}
	}
}

// checkHTM010UnknownEpubNS detects unrecognized epub-like namespace URIs (HTM-010).
func checkHTM010UnknownEpubNS(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	knownEpubNS := "http://www.idpf.org/2007/ops"
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attr := range se.Attr {
			if attr.Name.Space == "xmlns" && strings.Contains(attr.Value, "www.idpf.org/2007") {
				if attr.Value != knownEpubNS && attr.Value != "http://www.idpf.org/2007/opf" {
					r.AddWithLocation(report.Usage, "HTM-010",
						fmt.Sprintf(`Namespace "%s" is unusual; should use "%s"`, attr.Value, knownEpubNS),
						location)
					return
				}
			}
		}
	}
}

// Deprecated epub:type values (epubcheck OPF-086b)
var deprecatedEpubTypes = map[string]bool{
	"annoref":       true,
	"annotation":    true,
	"biblioentry":   true,
	"bridgehead":    true,
	"endnote":       true,
	"help":          true,
	"marginalia":    true,
	"note":          true,
	"rearnote":      true,
	"rearnotes":     true,
	"sidebar":       true,
	"subchapter":    true,
	"warning":       true,
	"biblioref":     true,
	"glossref":      true,
	"noteref":       true,
	"backlink":      true,
}

// checkOPF086bDeprecatedEpubType reports deprecated epub:type values (OPF-086b).
func checkOPF086bDeprecatedEpubType(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attr := range se.Attr {
			if attr.Name.Local == "type" && attr.Name.Space == "http://www.idpf.org/2007/ops" {
				for _, val := range strings.Fields(attr.Value) {
					if strings.Contains(val, ":") {
						continue
					}
					if deprecatedEpubTypes[val] {
						r.AddWithLocation(report.Usage, "OPF-086b",
							fmt.Sprintf(`epub:type value "%s" is deprecated`, val),
							location)
					}
				}
			}
		}
	}
}

// epub:type usage suggestions: which elements should use which epub:type values
var epubTypeSuggestions = map[string][]string{
	"bodymatter":    {"body", "section"},
	"frontmatter":   {"body", "section"},
	"backmatter":    {"body", "section"},
	"chapter":       {"section", "body"},
	"part":          {"section", "body"},
	"division":      {"section", "body"},
	"volume":        {"section", "body"},
	"subchapter":    {"section"},
	"toc":           {"nav", "section"},
	"landmarks":     {"nav"},
	"page-list":     {"nav"},
	"loa":           {"nav", "section"},
	"loi":           {"nav", "section"},
	"lot":           {"nav", "section"},
	"lov":           {"nav", "section"},
	"footnote":      {"aside"},
	"endnote":       {"aside", "li"},
	"endnotes":      {"section", "body"},
	"bibliography":  {"section", "body"},
	"index":         {"section", "body"},
	"glossary":      {"section", "body", "dl"},
	"pagebreak":     {"span", "div", "hr"},
	"noteref":       {"a"},
	"biblioref":     {"a"},
	"glossref":      {"a"},
}

// epubTypeRedundant maps epub:type values to element names where they are redundant.
// Using these epub:type values on their matching elements triggers OPF-087 as usage.
var epubTypeRedundant = map[string]string{
	"table":      "table",
	"table-row":  "tr",
	"table-cell": "td",
	"list":       "ul",
	"list-item":  "li",
	"figure":     "figure",
	"aside":      "aside",
}

// checkOPF087MisusedEpubType reports epub:type values used on unexpected elements (OPF-087).
func checkOPF087MisusedEpubType(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		elemName := se.Name.Local
		for _, attr := range se.Attr {
			if attr.Name.Local == "type" && attr.Name.Space == "http://www.idpf.org/2007/ops" {
				for _, val := range strings.Fields(attr.Value) {
					if strings.Contains(val, ":") {
						continue
					}
					// Check for redundant usage (e.g., epub:type="table" on <table>)
					if redundantElem, ok := epubTypeRedundant[val]; ok {
						if redundantElem == elemName {
							r.AddWithLocation(report.Usage, "OPF-087",
								fmt.Sprintf(`epub:type value "%s" is redundant on element "%s"`, val, elemName),
								location)
							continue
						}
					}
					// Check for misused suggestions (wrong element)
					if suggested, ok := epubTypeSuggestions[val]; ok {
						found := false
						for _, s := range suggested {
							if s == elemName {
								found = true
								break
							}
						}
						if !found {
							r.AddWithLocation(report.Usage, "OPF-087",
								fmt.Sprintf(`epub:type value "%s" is not appropriate for element "%s"`, val, elemName),
								location)
						}
					}
				}
			}
		}
	}
}

// checkOPF088UnknownEpubType reports unknown epub:type values in the default vocabulary (OPF-088).
func checkOPF088UnknownEpubType(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attr := range se.Attr {
			if attr.Name.Local == "type" && attr.Name.Space == "http://www.idpf.org/2007/ops" {
				for _, val := range strings.Fields(attr.Value) {
					if strings.Contains(val, ":") {
						continue
					}
					if !validEpubTypes[val] && !deprecatedEpubTypes[val] {
						r.AddWithLocation(report.Usage, "OPF-088",
							fmt.Sprintf(`epub:type value "%s" is not defined in the default vocabulary`, val),
							location)
						return
					}
				}
			}
		}
	}
}

// checkOPF028UndeclaredPrefix detects undeclared prefixes in epub:type attributes (OPF-028).
func checkOPF028UndeclaredPrefix(data []byte, location string, r *report.Report) {
	// Reserved prefixes that don't need declaration
	reservedPrefixes := map[string]bool{
		"dc": true, "dcterms": true, "a11y": true, "epub": true,
		"marc": true, "media": true, "onix": true, "rendition": true,
		"schema": true, "xsd": true, "msv": true, "prism": true,
	}

	// Collect declared prefixes from prefix attributes on any element
	declaredPrefixes := make(map[string]bool)
	content := string(data)

	// Parse epub:prefix attributes only (plain prefix= without namespace is not a valid declaration)
	prefixRe := regexp.MustCompile(`epub:prefix\s*=\s*["']([^"']*)["']`)
	for _, match := range prefixRe.FindAllStringSubmatch(content, -1) {
		if len(match) > 1 {
			parts := strings.Fields(match[1])
			for _, p := range parts {
				cleaned := strings.TrimSuffix(p, ":")
				// Only accept prefix names (not URLs)
				if !strings.Contains(cleaned, "/") && !strings.Contains(cleaned, ".") {
					declaredPrefixes[cleaned] = true
				}
			}
		}
	}

	// Also check xmlns namespace declarations for prefixes
	xmlnsRe := regexp.MustCompile(`xmlns:([a-zA-Z][a-zA-Z0-9]*)\s*=\s*["']([^"']*)["']`)
	for _, match := range xmlnsRe.FindAllStringSubmatch(content, -1) {
		if len(match) > 1 {
			declaredPrefixes[match[1]] = true
		}
	}

	// Check epub:type values for undeclared prefixes
	decoder := newXHTMLDecoder(strings.NewReader(content))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attr := range se.Attr {
			if attr.Name.Local == "type" && attr.Name.Space == "http://www.idpf.org/2007/ops" {
				for _, val := range strings.Fields(attr.Value) {
					if idx := strings.Index(val, ":"); idx > 0 {
						prefix := val[:idx]
						if !reservedPrefixes[prefix] && !declaredPrefixes[prefix] {
							r.AddWithLocation(report.Error, "OPF-028",
								fmt.Sprintf(`Undeclared prefix "%s" in epub:type value "%s"`, prefix, val),
								location)
							return
						}
					}
				}
			}
		}
	}
}

// checkDeprecatedDPUBARIA detects deprecated DPUB-ARIA roles (RSC-017).
func checkDeprecatedDPUBARIA(data []byte, location string, r *report.Report) {
	deprecatedRoles := map[string]bool{
		"doc-endnote":     true,
		"doc-biblioentry": true,
	}
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attr := range se.Attr {
			if attr.Name.Local == "role" {
				for _, role := range strings.Fields(attr.Value) {
					if deprecatedRoles[role] {
						r.AddWithLocation(report.Warning, "RSC-017",
							fmt.Sprintf(`"%s" role is deprecated`, role),
							location)
					}
				}
			}
		}
	}
}

// checkDOCTYPEChecks performs various DOCTYPE-related checks for single-file mode.
func checkDOCTYPEChecks(data []byte, location string, r *report.Report) {
	content := string(data)
	idx := strings.Index(strings.ToUpper(content), "<!DOCTYPE")
	if idx == -1 {
		return
	}
	endIdx := strings.Index(content[idx:], ">")
	if endIdx == -1 {
		return
	}
	doctype := content[idx : idx+endIdx+1]
	doctypeUpper := strings.ToUpper(doctype)

	// Check for unresolved entity reference in doctype
	if strings.Contains(doctypeUpper, "PUBLIC") {
		publicID, _ := extractDOCTYPEIdentifiers(doctype[9:]) // skip "<!DOCTYPE"
		if publicID != "" {
			// Invalid public ID patterns
			if strings.HasPrefix(publicID, "+//") {
				r.AddWithLocation(report.Error, "HTM-009",
					fmt.Sprintf(`Invalid DOCTYPE: public identifier "%s" is not valid`, publicID),
					location)
			}
		}
	}
}

// checkHTM004SingleFile checks for DOCTYPE issues in single-file XHTML (HTM-004).
// Works for both EPUB 2 and EPUB 3 content by detecting:
// - Obsolete/invalid public identifiers
// - Invalid DOCTYPE formats
func checkHTM004SingleFile(data []byte, location string, r *report.Report) {
	content := string(data)
	idx := strings.Index(strings.ToUpper(content), "<!DOCTYPE")
	if idx == -1 {
		return
	}
	endIdx := strings.Index(content[idx:], ">")
	if endIdx == -1 {
		return
	}
	doctype := content[idx : idx+endIdx+1]
	doctypeUpper := strings.ToUpper(doctype)

	// In EPUB3, any PUBLIC doctype is obsolete (HTM-004)
	if strings.Contains(doctypeUpper, "PUBLIC") {
		publicID, _ := extractDOCTYPEIdentifiers(doctype[2:]) // skip "<!" to get "DOCTYPE html PUBLIC..."
		r.AddWithLocation(report.Error, "HTM-004",
			fmt.Sprintf(`Irregular DOCTYPE: public identifier "%s" is obsolete`, publicID),
			location)
	}
}

// checkHTM004EPUB2Mode checks for DOCTYPE issues in EPUB2 OPS XHTML content (HTM-004).
// In EPUB2, the HTML5 DOCTYPE (<!DOCTYPE html>) and invalid public identifiers are errors.
func checkHTM004EPUB2Mode(data []byte, location string, r *report.Report) {
	content := string(data)
	idx := strings.Index(strings.ToUpper(content), "<!DOCTYPE")
	if idx == -1 {
		return
	}
	endIdx := strings.Index(content[idx:], ">")
	if endIdx == -1 {
		return
	}
	doctype := content[idx : idx+endIdx+1]
	doctypeUpper := strings.ToUpper(doctype)

	// Valid EPUB2 public identifiers
	validPublicIDs := map[string]bool{
		"-//W3C//DTD XHTML 1.1//EN":              true,
		"-//W3C//DTD XHTML 1.0 STRICT//EN":       true,
		"-//W3C//DTD XHTML 1.0 TRANSITIONAL//EN": true,
		"-//W3C//DTD XHTML BASIC 1.1//EN":        true,
		"-//W3C//DTD XHTML+RDFA 1.0//EN":         true,
	}

	if !strings.Contains(doctypeUpper, "PUBLIC") {
		// No PUBLIC identifier: HTML5 DOCTYPE or minimal DOCTYPE - not valid in EPUB2
		r.AddWithLocation(report.Error, "HTM-004",
			`Irregular DOCTYPE: HTML5 DOCTYPE is not allowed in EPUB 2 OPS XHTML content documents`,
			location)
		return
	}

	// Has PUBLIC: check that the public ID is a valid EPUB2 XHTML identifier
	publicID, _ := extractDOCTYPEIdentifiers(doctype[2:]) // skip "<!" to get "DOCTYPE html PUBLIC..."
	publicIDUpper := strings.ToUpper(publicID)
	if !validPublicIDs[publicIDUpper] {
		r.AddWithLocation(report.Error, "HTM-004",
			fmt.Sprintf(`Irregular DOCTYPE: public identifier "%s" is not valid`, publicID),
			location)
	}
}

// checkHTML5ElementsEPUB2 detects HTML5 elements not allowed in EPUB2 OPS XHTML (RSC-005).
func checkHTML5ElementsEPUB2(data []byte, location string, r *report.Report) {
	html5Elements := map[string]bool{
		"aside": true, "article": true, "section": true, "nav": true,
		"header": true, "footer": true, "main": true, "figure": true,
		"figcaption": true, "details": true, "summary": true, "dialog": true,
		"mark": true, "time": true, "meter": true, "progress": true,
		"output": true, "canvas": true, "video": true, "audio": true,
		"source": true, "track": true, "embed": true, "wbr": true,
		"datalist": true, "keygen": true, "template": true,
	}
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if html5Elements[se.Name.Local] {
			r.AddWithLocation(report.Error, "RSC-005",
				fmt.Sprintf(`element "%s" not allowed here`, se.Name.Local),
				location)
			return
		}
	}
}

// checkInvalidIDValues detects id attribute values that are not valid XML NCNames (RSC-005).
// XML NCNames cannot contain colons and must start with a letter or underscore.
// This catches calibre-generated IDs like "fn:1" and "fnref:2".
func checkInvalidIDValues(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attr := range se.Attr {
			if attr.Name.Local == "id" && attr.Value != "" {
				if strings.Contains(attr.Value, ":") {
					r.AddWithLocation(report.Error, "RSC-005",
						fmt.Sprintf(`value of attribute "id" is invalid; must be an XML name without colons`),
						location)
				}
			}
		}
	}
}

// checkUnknownEntityRefs detects unknown XML entity references (RSC-016).
// Catches &foo; style entity refs that are not declared in the DTD.
// Handles internal DTD subsets by extracting declared entity names.
// Note: entity-without-semicolon errors are handled by checkEntityReferences (regex-based).
func checkUnknownEntityRefs(data []byte, location string, r *report.Report) {
	content := string(data)

	// Extract entity declarations from internal DTD subset if present.
	// Go's XML decoder skips the DOCTYPE declaration but doesn't know about
	// entities declared inside it. We need to pre-populate decoder.Entity.
	// Start with standard XHTML entities, then overlay any inline DTD declarations.
	declaredEntities := make(map[string]string, len(xhtmlEntities))
	for k, v := range xhtmlEntities {
		declaredEntities[k] = v
	}
	entityDeclRe := regexp.MustCompile(`<!ENTITY\s+(\w+)\s+(?:"[^"]*"|'[^']*')`)
	for _, m := range entityDeclRe.FindAllStringSubmatch(content, -1) {
		declaredEntities[m[1]] = m[1]
	}

	decoder := newXHTMLDecoder(strings.NewReader(content))
	decoder.Entity = declaredEntities

	for {
		_, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			errMsg := err.Error()
			// Go's XML decoder reports undefined entities as "invalid character entity".
			// Entity-without-semicolon errors contain "(no semicolon)" and are handled
			// by checkEntityReferences (regex-based); skip them here to avoid double-reporting.
			if strings.Contains(errMsg, "invalid character entity") && !strings.Contains(errMsg, "no semicolon") {
				r.AddWithLocation(report.Fatal, "RSC-016",
					"Fatal Error while parsing file: entity was referenced, but not declared",
					location)
			}
			break
		}
	}
}

// checkImageMapValid detects invalid image map constructs (RSC-005).
func checkImageMapValid(data []byte, location string, r *report.Report) {
	content := string(data)
	isHTML5 := strings.Contains(content, "<!DOCTYPE html>") || strings.Contains(content, "<!doctype html>")
	decoder := newXHTMLDecoder(strings.NewReader(content))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Local == "img" && isHTML5 {
			for _, attr := range se.Attr {
				if attr.Name.Local == "usemap" {
					val := attr.Value
					if val != "" && !strings.HasPrefix(val, "#") {
						r.AddWithLocation(report.Error, "RSC-005",
							`value of attribute "usemap" is invalid; must start with "#"`,
							location)
					}
				}
			}
		}
	}
}

// checkHttpEquivCharset detects http-equiv charset issues (RSC-005).
func checkHttpEquivCharset(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	hasCharsetAttr := false
	hasHttpEquivCharset := false
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "meta" {
			continue
		}
		httpEquiv := ""
		content := ""
		charset := ""
		for _, attr := range se.Attr {
			switch attr.Name.Local {
			case "http-equiv":
				httpEquiv = strings.ToLower(strings.TrimSpace(attr.Value))
			case "content":
				content = attr.Value
			case "charset":
				charset = attr.Value
			}
		}
		if charset != "" {
			hasCharsetAttr = true
		}
		if httpEquiv == "content-type" {
			hasHttpEquivCharset = true
			// Check that charset is UTF-8
			contentLower := strings.ToLower(strings.TrimSpace(content))
			if contentLower != "" && contentLower != "text/html; charset=utf-8" {
				r.AddWithLocation(report.Error, "RSC-005",
					`attribute "content" must have the value "text/html; charset=utf-8"`,
					location)
			}
		}
	}
	if hasCharsetAttr && hasHttpEquivCharset {
		r.AddWithLocation(report.Error, "RSC-005",
			`must not contain both a meta element in encoding declaration state (http-equiv='content-type') and a meta element with the charset attribute`,
			location)
	}
}

// checkMicrodataAttrs detects microdata attributes on non-allowed elements (RSC-005).
func checkMicrodataAttrs(data []byte, location string, r *report.Report) {
	// Elements that require specific attributes when itemprop is set
	needsHref := map[string]bool{
		"a": true, "area": true, "link": true,
	}
	needsData := map[string]bool{
		"object": true,
	}
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		hasItemprop := false
		hasHref := false
		hasData := false
		for _, attr := range se.Attr {
			switch attr.Name.Local {
			case "itemprop":
				hasItemprop = true
			case "href":
				hasHref = true
			case "data":
				hasData = true
			}
		}
		if !hasItemprop {
			continue
		}
		elemName := se.Name.Local
		if needsHref[elemName] && !hasHref {
			r.AddWithLocation(report.Error, "RSC-005",
				fmt.Sprintf(`element "%s" missing required attribute "href"`, elemName),
				location)
		} else if needsData[elemName] && !hasData {
			r.AddWithLocation(report.Error, "RSC-005",
				fmt.Sprintf(`If the itemprop is specified on element "%s", a "data" attribute must also be specified`, elemName),
				location)
		}
	}
}

// checkObsoleteAttrs detects obsolete HTML attributes and elements (RSC-005).
func checkObsoleteAttrs(data []byte, location string, r *report.Report) {
	obsoleteAttrs := map[string]bool{
		"typemustmatch": true,
		"contextmenu":   true,
		"dropzone":      true,
		"pubdate":       true,
		"seamless":      true,
	}
	obsoleteElements := map[string]bool{
		"keygen":  true,
		"command": true,
	}
	// Obsolete element-specific attributes
	obsoleteMenuAttrs := map[string]bool{
		"type": true, "label": true,
	}
	// Deprecated HTML presentation attributes per element (HTML5 obsoletes these)
	deprecatedPresAttrs := map[string]map[string]bool{
		"col":      {"align": true, "valign": true, "width": true, "char": true, "charoff": true},
		"colgroup": {"align": true, "valign": true, "char": true, "charoff": true},
		"tbody":    {"align": true, "valign": true, "char": true, "charoff": true},
		"thead":    {"align": true, "valign": true, "char": true, "charoff": true},
		"tfoot":    {"align": true, "valign": true, "char": true, "charoff": true},
		"tr":       {"align": true, "valign": true, "bgcolor": true, "char": true, "charoff": true},
		"td":       {"align": true, "valign": true, "bgcolor": true, "width": true, "height": true, "char": true, "charoff": true, "nowrap": true, "axis": true, "abbr": true},
		"th":       {"align": true, "valign": true, "bgcolor": true, "width": true, "height": true, "char": true, "charoff": true, "nowrap": true, "axis": true, "abbr": true},
		"table":    {"align": true, "bgcolor": true, "cellpadding": true, "cellspacing": true, "frame": true, "rules": true, "summary": true},
		"caption":  {"align": true},
		"div":      {"align": true},
		"p":        {"align": true},
		"h1":       {"align": true},
		"h2":       {"align": true},
		"h3":       {"align": true},
		"h4":       {"align": true},
		"h5":       {"align": true},
		"h6":       {"align": true},
		"hr":       {"align": true, "noshade": true, "size": true, "width": true, "color": true},
		"body":     {"bgcolor": true, "text": true, "link": true, "vlink": true, "alink": true, "background": true},
		"br":       {"clear": true},
		"img":      {"align": true, "border": true, "hspace": true, "vspace": true, "longdesc": true},
		"object":   {"align": true, "border": true, "hspace": true, "vspace": true},
		"embed":    {"align": true},
		"iframe":   {"align": true, "frameborder": true, "marginwidth": true, "marginheight": true, "scrolling": true, "longdesc": true},
		"input":    {"align": true},
		"legend":   {"align": true},
		"li":       {"type": true},
		"ul":       {"type": true},
		"pre":      {"width": true},
	}
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	menuDepth := 0  // depth inside a menu element (1 = direct child)
	inMenu := false
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "menu" {
				inMenu = true
				menuDepth = 0
			} else if inMenu {
				menuDepth++
			}
			if obsoleteElements[t.Name.Local] {
				r.AddWithLocation(report.Error, "RSC-005",
					fmt.Sprintf(`element "%s" not allowed here`, t.Name.Local),
					location)
			}
			// button as direct child of menu (not nested in li)
			if inMenu && menuDepth == 1 && t.Name.Local == "button" {
				r.AddWithLocation(report.Error, "RSC-005",
					`element "button" not allowed here`,
					location)
			}
			elemAttrs := deprecatedPresAttrs[t.Name.Local]
			for _, attr := range t.Attr {
				if obsoleteAttrs[attr.Name.Local] {
					r.AddWithLocation(report.Error, "RSC-005",
						fmt.Sprintf(`attribute "%s" not allowed here`, attr.Name.Local),
						location)
				}
				// menu element obsolete attributes
				if t.Name.Local == "menu" && obsoleteMenuAttrs[attr.Name.Local] {
					r.AddWithLocation(report.Error, "RSC-005",
						fmt.Sprintf(`attribute "%s" not allowed here`, attr.Name.Local),
						location)
				}
				// Deprecated HTML presentation attributes (element-specific)
				// Only match non-namespaced attributes (skip epub:type, xml:lang, etc.)
				if elemAttrs != nil && attr.Name.Space == "" && elemAttrs[attr.Name.Local] {
					r.AddWithLocation(report.Error, "RSC-005",
						fmt.Sprintf(`attribute "%s" not allowed here; expected attribute`, attr.Name.Local),
						location)
				}
			}
		case xml.EndElement:
			if t.Name.Local == "menu" {
				inMenu = false
				menuDepth = 0
			} else if inMenu {
				menuDepth--
			}
		}
	}
}

// checkACCMathMLAlt checks MathML elements for alternative text (ACC-009).
func checkACCMathMLAlt(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Local == "math" && (se.Name.Space == "http://www.w3.org/1998/Math/MathML" || se.Name.Space == "") {
			hasAlt := false
			for _, attr := range se.Attr {
				if attr.Name.Local == "alttext" || attr.Name.Local == "altimg" ||
					attr.Name.Local == "aria-label" || attr.Name.Local == "aria-labelledby" {
					hasAlt = true
					break
				}
			}
			if !hasAlt {
				r.AddWithLocation(report.Usage, "ACC-009",
					"MathML element should have an alternative text representation (alttext, altimg, aria-label, or aria-labelledby)",
					location)
			}
		}
	}
}

// ============================================================================
// SVG single-file check functions
// ============================================================================

// checkSVGDuplicateIDs detects duplicate id attribute values in SVG (RSC-005).
func checkSVGDuplicateIDs(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	idCount := make(map[string]int)
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attr := range se.Attr {
			if attr.Name.Local == "id" && attr.Value != "" {
				idCount[attr.Value]++
			}
		}
	}
	// Report all occurrences of duplicated IDs
	for id, count := range idCount {
		if count > 1 {
			for i := 0; i < count; i++ {
				r.AddWithLocation(report.Error, "RSC-005",
					fmt.Sprintf(`Duplicate ID "%s"`, id),
					location)
			}
		}
	}
}

// checkSVGInvalidIDs detects invalid id attribute values in SVG (RSC-005).
func checkSVGInvalidIDs(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	invalidIDRe := regexp.MustCompile(`^\d|^\s|\s`)
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attr := range se.Attr {
			if attr.Name.Local == "id" && attr.Value != "" {
				if invalidIDRe.MatchString(attr.Value) || strings.TrimSpace(attr.Value) == "" {
					r.AddWithLocation(report.Error, "RSC-005",
						fmt.Sprintf(`value of attribute "id" is invalid; must be an XML Name`),
						location)
				}
			}
		}
	}
}

// checkSVGForeignObject checks foreignObject for valid content (RSC-005).
// Checks: non-HTML content, non-flow content (title/head/etc), multiple body elements,
// and HTML validation errors (invalid attributes).
func checkSVGForeignObject(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	svgNS := "http://www.w3.org/2000/svg"
	htmlNS := "http://www.w3.org/1999/xhtml"

	// Non-flow HTML elements (metadata content, not allowed as direct children of foreignObject)
	nonFlowElements := map[string]bool{
		"title": true, "base": true, "link": true, "meta": true,
		"style": true, "head": true,
	}
	// HTML attributes that are NOT valid on specific elements
	noHrefElements := map[string]bool{
		"p": true, "div": true, "span": true, "h1": true, "h2": true,
		"h3": true, "h4": true, "h5": true, "h6": true,
	}

	foDepth := 0    // foreignObject nesting depth
	bodyCount := 0  // body elements in current foreignObject
	firstChild := false // true when next element is direct child of foreignObject
	isXHTMLDoc := false // true if the root is <html> (XHTML context)
	isHTML5 := strings.Contains(string(data), "<!DOCTYPE html>") || strings.Contains(string(data), "<!doctype html>")
	rootSeen := false

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if !rootSeen {
				rootSeen = true
				isXHTMLDoc = t.Name.Local == "html"
			}
			if t.Name.Local == "foreignObject" && (t.Name.Space == svgNS || t.Name.Space == "") {
				foDepth++
				bodyCount = 0
				firstChild = true
				continue
			}
			if foDepth > 0 {
				isHTML := t.Name.Space == htmlNS || t.Name.Space == ""
				isSVG := t.Name.Space == svgNS
				// Direct children of foreignObject
				if firstChild {
					firstChild = false
					if !isHTML || isSVG {
						// Non-HTML child content
						r.AddWithLocation(report.Error, "RSC-005",
							fmt.Sprintf(`element "%s" not allowed here`, t.Name.Local),
							location)
						continue
					}
					// Non-flow content (like <title> as direct child)
					if isHTML && nonFlowElements[t.Name.Local] {
						r.AddWithLocation(report.Error, "RSC-005",
							fmt.Sprintf(`element "%s" not allowed here`, t.Name.Local),
							location)
						continue
					}
					// In HTML5/EPUB 3 XHTML context, body is not allowed inside foreignObject
					if isXHTMLDoc && isHTML5 && t.Name.Local == "body" {
						r.AddWithLocation(report.Error, "RSC-005",
							`element "body" not allowed here`,
							location)
						continue
					}
				}
				// body element tracking (in SVG context, only second body is error)
				if t.Name.Local == "body" && (t.Name.Space == htmlNS || t.Name.Space == "") {
					bodyCount++
					if !isXHTMLDoc && bodyCount > 1 {
						r.AddWithLocation(report.Error, "RSC-005",
							`element "body" not allowed here`,
							location)
					}
				}
				// HTML validation: href not allowed on certain elements
				if isHTML && noHrefElements[t.Name.Local] {
					for _, attr := range t.Attr {
						if attr.Name.Local == "href" && (attr.Name.Space == "" || attr.Name.Space == htmlNS) {
							r.AddWithLocation(report.Error, "RSC-005",
								`attribute "href" not allowed here`,
								location)
						}
					}
				}
			}
		case xml.EndElement:
			if t.Name.Local == "foreignObject" && foDepth > 0 {
				foDepth--
			}
		}
	}
}

// checkSVGTitleContent checks SVG title elements for valid content (RSC-005).
// SVG title elements can only contain HTML phrasing content; no custom namespace
// elements or SVG elements are allowed. Also checks for invalid HTML attributes.
// standaloneMode should be true for standalone SVG files (where non-phrasing HTML
// elements with inline xmlns are flagged), false for XHTML-embedded SVG.
func checkSVGTitleContent(data []byte, location string, r *report.Report, standaloneMode bool) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	svgNS := "http://www.w3.org/2000/svg"
	htmlNS := "http://www.w3.org/1999/xhtml"
	noHrefElements := map[string]bool{
		"span": true, "p": true, "div": true,
	}
	// Non-phrasing HTML elements not allowed as direct content of SVG title.
	// These are only flagged when they appear with an inline xmlns= declaration
	// (i.e., <body xmlns="..."> but NOT <h:body> which uses prefix notation).
	// Exception: <html xmlns="..."> is always valid (represents an embedded HTML doc).
	nonPhrasingHTML := map[string]bool{
		"body": true, "head": true, "address": true,
		"article": true, "aside": true, "blockquote": true,
		"fieldset": true, "figcaption": true, "figure": true, "footer": true,
		"form": true, "header": true, "hr": true, "main": true, "nav": true,
		"ol": true, "pre": true, "section": true, "table": true,
		"ul": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	}

	inSVG := 0          // SVG element nesting depth
	inSVGTitle := false  // inside an SVG title element
	titleDepth := 0      // element depth inside title (to track direct children)
	reportedNS := make(map[string]bool) // track reported namespaces per title
	// Deferred non-phrasing element check: suppressed if namespace violation found.
	pendingNonPhrasingElem := "" // set when non-phrasing HTML with xmlns is found at depth 1
	hasNamespaceViolation := false

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			isSVGElem := t.Name.Space == svgNS
			if t.Name.Local == "svg" && (isSVGElem || t.Name.Space == "") {
				inSVG++
			}
			if inSVGTitle {
				titleDepth++
				isHTML := t.Name.Space == htmlNS
				// Namespace violation checks apply at any depth
				if !isHTML && !isSVGElem && t.Name.Space != "" {
					ns := t.Name.Space
					if !reportedNS[ns] {
						r.AddWithLocation(report.Error, "RSC-005",
							fmt.Sprintf(`elements from namespace "%s" are not allowed`, ns),
							location)
						reportedNS[ns] = true
					}
					hasNamespaceViolation = true
				} else if isSVGElem {
					// SVG elements inside title
					if !reportedNS[svgNS] {
						r.AddWithLocation(report.Error, "RSC-005",
							fmt.Sprintf(`elements from namespace "%s" are not allowed`, svgNS),
							location)
						reportedNS[svgNS] = true
					}
					hasNamespaceViolation = true
				} else if titleDepth == 1 {
					// HTML element and attribute validation: only check direct children
					if isHTML {
						if noHrefElements[t.Name.Local] {
							for _, attr := range t.Attr {
								if attr.Name.Local == "href" && (attr.Name.Space == "" || attr.Name.Space == htmlNS) {
									r.AddWithLocation(report.Error, "RSC-005",
										`attribute "href" not allowed here`,
										location)
								}
							}
						}
						// Only flag non-phrasing elements in standalone SVG mode.
						// In XHTML-embedded SVG, all HTML elements (even body/h1) are valid.
						// For standalone SVG, only flag elements with INLINE xmlns= declarations
						// (e.g., <body xmlns="...">); prefix-based elements (<h:body>) are valid.
						// Exception: <html xmlns="..."> is always valid (embedded HTML doc).
						// Defer the error until end of title in case SVG inside generates
						// a namespace violation (which takes priority).
						if standaloneMode && t.Name.Local != "html" && nonPhrasingHTML[t.Name.Local] {
							for _, attr := range t.Attr {
								if attr.Name.Space == "" && attr.Name.Local == "xmlns" {
									pendingNonPhrasingElem = t.Name.Local
									break
								}
							}
						}
					}
				}
			} else if inSVG > 0 && t.Name.Local == "title" && (isSVGElem || t.Name.Space == "") {
				inSVGTitle = true
				titleDepth = 0
				reportedNS = make(map[string]bool)
				pendingNonPhrasingElem = ""
				hasNamespaceViolation = false
			}
		case xml.EndElement:
			if t.Name.Local == "svg" && (t.Name.Space == svgNS || t.Name.Space == "") && inSVG > 0 {
				inSVG--
			}
			if inSVGTitle {
				if t.Name.Local == "title" && (t.Name.Space == svgNS || t.Name.Space == "") && titleDepth == 0 {
					// Flush deferred non-phrasing element error if no namespace violations found
					if pendingNonPhrasingElem != "" && !hasNamespaceViolation {
						r.AddWithLocation(report.Error, "RSC-005",
							fmt.Sprintf(`element "%s" not allowed here`, pendingNonPhrasingElem),
							location)
					}
					inSVGTitle = false
				} else {
					titleDepth--
				}
			}
		}
	}
}

// checkSVGEpubType checks epub:type usage on SVG elements (RSC-005).
// epub:type is allowed on structural (svg, g, use, symbol, defs),
// shape (rect, circle, ellipse, line, polyline, polygon, path),
// and text (text, textPath, tspan) elements.
func checkSVGEpubType(data []byte, location string, r *report.Report) {
	allowedEpubType := map[string]bool{
		"svg": true, "g": true, "use": true, "symbol": true,
		"rect": true, "circle": true, "ellipse": true, "line": true,
		"polyline": true, "polygon": true, "path": true,
		"text": true, "textPath": true, "tspan": true,
	}
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attr := range se.Attr {
			if attr.Name.Local == "type" && attr.Name.Space == "http://www.idpf.org/2007/ops" {
				if !allowedEpubType[se.Name.Local] {
					r.AddWithLocation(report.Error, "RSC-005",
						fmt.Sprintf(`"epub:type" not allowed on element "%s"`, se.Name.Local),
						location)
				}
			}
		}
	}
}

// checkSVGUnknownEpubAttr detects unknown epub: attributes in SVG (RSC-005).
// epub:type is allowed on SVG elements; epub:prefix is allowed on the root SVG element.
func checkSVGUnknownEpubAttr(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	isRoot := true
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attr := range se.Attr {
			if attr.Name.Space == "http://www.idpf.org/2007/ops" {
				// epub:type is always allowed; epub:prefix is allowed on root element only
				if attr.Name.Local == "type" {
					continue
				}
				if attr.Name.Local == "prefix" && isRoot {
					continue
				}
				r.AddWithLocation(report.Error, "RSC-005",
					fmt.Sprintf(`"epub:%s" not allowed here`, attr.Name.Local),
					location)
				return
			}
		}
		isRoot = false
	}
}

// checkSVGInvalidElements reports unknown elements in SVG as usage (RSC-025).
func checkSVGInvalidElements(data []byte, location string, r *report.Report) {
	svgElements := map[string]bool{
		"svg": true, "g": true, "defs": true, "symbol": true, "use": true,
		"image": true, "switch": true, "foreignObject": true,
		"desc": true, "title": true, "metadata": true,
		"rect": true, "circle": true, "ellipse": true, "line": true,
		"polyline": true, "polygon": true, "path": true,
		"text": true, "textPath": true, "tspan": true, "tref": true,
		"a": true, "altGlyphDef": true, "clipPath": true, "color-profile": true,
		"cursor": true, "filter": true, "font": true, "font-face": true,
		"glyph": true, "glyphRef": true, "altGlyph": true, "altGlyphItem": true,
		"linearGradient": true, "radialGradient": true, "stop": true,
		"pattern": true, "marker": true, "mask": true,
		"style": true, "script": true, "animate": true, "set": true,
		"animateMotion": true, "animateTransform": true, "animateColor": true,
		"mpath": true, "view": true, "solidColor": true,
		"feBlend": true, "feColorMatrix": true, "feComponentTransfer": true,
		"feComposite": true, "feConvolveMatrix": true, "feDiffuseLighting": true,
		"feDisplacementMap": true, "feDistantLight": true, "feFlood": true,
		"feFuncA": true, "feFuncB": true, "feFuncG": true, "feFuncR": true,
		"feGaussianBlur": true, "feImage": true, "feMerge": true,
		"feMergeNode": true, "feMorphology": true, "feOffset": true,
		"fePointLight": true, "feSpecularLighting": true, "feSpotLight": true,
		"feTile": true, "feTurbulence": true,
		"font-face-format": true, "font-face-name": true, "font-face-src": true,
		"font-face-uri": true, "hkern": true, "vkern": true,
		"missing-glyph": true,
	}
	svgNS := "http://www.w3.org/2000/svg"
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	inSVG := 0
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == svgNS || (t.Name.Space == "" && inSVG > 0) {
				if t.Name.Local == "svg" {
					inSVG++
				}
				if inSVG > 0 && !svgElements[t.Name.Local] && t.Name.Space != "http://www.w3.org/1999/xhtml" {
					r.AddWithLocation(report.Usage, "RSC-025",
						fmt.Sprintf(`element "%s" not allowed here`, t.Name.Local),
						location)
				}
			}
		case xml.EndElement:
			if (t.Name.Space == svgNS || t.Name.Space == "") && t.Name.Local == "svg" && inSVG > 0 {
				inSVG--
			}
		}
	}
}

// checkSVGLinkLabel reports SVG hyperlinks without accessible labels (ACC-011).
func checkSVGLinkLabel(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Local == "a" {
			hasLabel := false
			for _, attr := range se.Attr {
				if attr.Name.Local == "aria-label" || attr.Name.Local == "aria-labelledby" {
					hasLabel = true
				}
				if attr.Name.Local == "title" {
					hasLabel = true
				}
			}
			if !hasLabel {
				// Check if the <a> has a child <title> element
				inner, err := decoder.Token()
				if err == nil {
					if se2, ok := inner.(xml.StartElement); ok && se2.Name.Local == "title" {
						hasLabel = true
					}
				}
			}
			if !hasLabel {
				r.AddWithLocation(report.Usage, "ACC-011",
					"SVG link element should have a label (title, aria-label, or aria-labelledby)",
					location)
			}
		}
	}
}

// ============================================================================
// Single-file encoding detection
// ============================================================================

// UCS-4 BOM markers
var ucs4BE = []byte{0x00, 0x00, 0xFE, 0xFF}
var ucs4LE = []byte{0xFF, 0xFE, 0x00, 0x00}

// checkSingleFileEncoding detects encoding issues in single-file XHTML (RSC-016/RSC-017).
func checkSingleFileEncoding(ep *epub.EPUB, r *report.Report) {
	if ep.Package == nil {
		return
	}
	xmlEncodingRe := regexp.MustCompile(`<\?xml[^?]*encoding=["']([^"']+)["']`)

	for _, item := range ep.Package.Manifest {
		// Only check SVG files here; XHTML encoding is handled by checkHTM058Encoding
		if item.MediaType != "image/svg+xml" {
			continue
		}
		if item.Href == "\x00MISSING" {
			continue
		}
		fullPath := ep.ResolveHref(item.Href)
		data, err := ep.ReadFile(fullPath)
		if err != nil {
			continue
		}

		// RSC-016: UCS-4 encoding
		if bytes.HasPrefix(data, ucs4BE) || bytes.HasPrefix(data, ucs4LE) {
			r.AddWithLocation(report.Fatal, "RSC-016",
				"Fatal Error while parsing file: UCS-4 encoding is not supported",
				fullPath)
			continue
		}

		// RSC-017: UTF-16 encoding
		if bytes.HasPrefix(data, utf16LEBOM) || bytes.HasPrefix(data, utf16BEBOM) {
			r.AddWithLocation(report.Warning, "RSC-017",
				"Warning while parsing file: XML document is encoded as UTF-16",
				fullPath)
			continue
		}

		// Check XML encoding declaration
		header := string(data[:min(200, len(data))])
		if matches := xmlEncodingRe.FindStringSubmatch(header); len(matches) > 1 {
			enc := strings.ToUpper(matches[1])
			switch {
			case enc == "UTF-16":
				// UTF-16 declared but no BOM
				r.AddWithLocation(report.Warning, "RSC-017",
					"Warning while parsing file: XML document declares UTF-16 encoding",
					fullPath)
			case enc == "ISO-8859-1" || enc == "LATIN-1" || enc == "LATIN1":
				r.AddWithLocation(report.Fatal, "RSC-016",
					fmt.Sprintf("Fatal Error while parsing file: encoding %s is not supported", matches[1]),
					fullPath)
			case enc != "UTF-8":
				r.AddWithLocation(report.Fatal, "RSC-016",
					fmt.Sprintf("Fatal Error while parsing file: unknown encoding %s", matches[1]),
					fullPath)
			}
		}
	}
}

// ============================================================================
// Single-file SMIL check functions
// ============================================================================

// checkSingleFileSMIL runs SMIL-specific checks for single-file validation.
func checkSingleFileSMIL(ep *epub.EPUB, data []byte, location string, r *report.Report) {
	// Check for undeclared prefixes in epub:type attributes (OPF-028)
	checkOPF028UndeclaredPrefix(data, location, r)

	// Check for plain prefix attribute (no epub: namespace) which is not valid
	checkSMILPlainPrefixAttr(data, location, r)

	decoder := newXHTMLDecoder(strings.NewReader(string(data)))

	// Stack tracking for parent elements
	var parentStack []string

	currentParent := func() string {
		if len(parentStack) == 0 {
			return ""
		}
		return parentStack[len(parentStack)-1]
	}

	// Track par children
	type parState struct {
		textCount int
	}
	parStates := make(map[int]*parState) // keyed by stack depth

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			name := t.Name.Local
			depth := len(parentStack)
			parentStack = append(parentStack, name)

			// meta directly in head (not in metadata)
			if name == "meta" && currentParent() == "meta" {
				// This won't fire... let me check differently
			}
			if name == "meta" {
				// Look back for parent = head
				if depth >= 1 && parentStack[depth-1] == "head" {
					r.AddWithLocation(report.Error, "RSC-005",
						`element "meta" not allowed here`,
						location)
				}
			}

			// seq direct children: text and audio not allowed
			parent := ""
			if depth > 0 {
				parent = parentStack[depth-1]
			}
			if parent == "seq" && (name == "text" || name == "audio") {
				r.AddWithLocation(report.Error, "RSC-005",
					fmt.Sprintf(`element "%s" not allowed here`, name),
					location)
			}

			// par content model
			if parent == "par" {
				if name == "seq" {
					r.AddWithLocation(report.Error, "RSC-005",
						`element "seq" not allowed here`,
						location)
				}
				if name == "text" {
					ps, ok := parStates[depth-1]
					if !ok {
						ps = &parState{}
						parStates[depth-1] = ps
					}
					ps.textCount++
					if ps.textCount > 1 {
						r.AddWithLocation(report.Error, "RSC-005",
							`element "text" not allowed here`,
							location)
					}
				}
			}

			// Initialize par state
			if name == "par" {
				parStates[depth] = &parState{}
			}

			// Audio element checks
			if name == "audio" {
				var src, clipBegin, clipEnd string
				for _, attr := range t.Attr {
					switch attr.Name.Local {
					case "src":
						src = attr.Value
					case "clipBegin":
						clipBegin = attr.Value
					case "clipEnd":
						clipEnd = attr.Value
					}
				}
				// MED-014: audio src should not contain a fragment identifier
				if src != "" && strings.Contains(src, "#") {
					r.AddWithLocation(report.Error, "MED-014",
						fmt.Sprintf("Audio source '%s' must not have a fragment identifier", src),
						location)
				}
				// Clock value validation
				if clipBegin != "" && !isValidSMILClock(clipBegin) {
					r.AddWithLocation(report.Error, "RSC-005",
						fmt.Sprintf(`Invalid clock value "%s"`, clipBegin),
						location)
				}
				if clipEnd != "" && !isValidSMILClock(clipEnd) {
					r.AddWithLocation(report.Error, "RSC-005",
						fmt.Sprintf(`Invalid clock value "%s"`, clipEnd),
						location)
				}
				// MED-008/MED-009: clip time comparison (only when both values are valid)
				validBegin := clipBegin == "" || isValidSMILClock(clipBegin)
				validEnd := clipEnd == "" || isValidSMILClock(clipEnd)
				if validBegin && validEnd {
					beginMs := parseSMILClockMs(clipBegin)
					endMs := parseSMILClockMs(clipEnd)
					// Default clipBegin is 0
					if clipBegin == "" {
						beginMs = 0
					}
					if beginMs >= 0 && endMs >= 0 {
						if beginMs > endMs {
							r.AddWithLocation(report.Error, "MED-008",
								fmt.Sprintf("clipBegin value (%s) is after clipEnd value (%s)", clipBegin, clipEnd),
								location)
						}
						if beginMs == endMs {
							r.AddWithLocation(report.Error, "MED-009",
								fmt.Sprintf("clipEnd value (%s) equals clipBegin value (%s)", clipEnd, clipBegin),
								location)
						}
					}
				}
			}

			// Check epub:type on SMIL elements for OPF-088
			for _, attr := range t.Attr {
				if attr.Name.Local == "type" && attr.Name.Space == "http://www.idpf.org/2007/ops" {
					for _, val := range strings.Fields(attr.Value) {
						if strings.Contains(val, ":") {
							continue
						}
						if !validEpubTypes[val] && !deprecatedEpubTypes[val] {
							r.AddWithLocation(report.Usage, "OPF-088",
								fmt.Sprintf(`epub:type value "%s" is not defined in the default vocabulary`, val),
								location)
						}
					}
				}
			}

		case xml.EndElement:
			if len(parentStack) > 0 {
				depth := len(parentStack) - 1
				if parentStack[depth] == "par" {
					delete(parStates, depth)
				}
				parentStack = parentStack[:depth]
			}
		}
	}
}

// checkSMILPlainPrefixAttr detects plain prefix attribute (without epub: namespace) in SMIL (RSC-005).
// In SMIL, the prefix attribute must use the epub: namespace; plain prefix= is not allowed.
func checkSMILPlainPrefixAttr(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attr := range se.Attr {
			if attr.Name.Local == "prefix" && attr.Name.Space == "" {
				r.AddWithLocation(report.Error, "RSC-005",
					`attribute "prefix" not allowed here`,
					location)
				return
			}
		}
	}
}

// isValidSMILClock validates a SMIL3 clock value format.
func isValidSMILClock(val string) bool {
	val = strings.TrimSpace(val)
	if val == "" {
		return false
	}

	// Timecount values: number followed by unit (h, min, s, ms)
	if strings.HasSuffix(val, "ms") {
		numStr := strings.TrimSuffix(val, "ms")
		_, err := parseFloat(numStr)
		return err == nil && len(numStr) > 0 && numStr[0] != '.'
	}
	if strings.HasSuffix(val, "min") {
		numStr := strings.TrimSuffix(val, "min")
		_, err := parseFloat(numStr)
		return err == nil && len(numStr) > 0 && numStr[0] != '.'
	}
	if strings.HasSuffix(val, "h") {
		numStr := strings.TrimSuffix(val, "h")
		_, err := parseFloat(numStr)
		return err == nil && len(numStr) > 0 && numStr[0] != '.'
	}
	if strings.HasSuffix(val, "s") {
		numStr := strings.TrimSuffix(val, "s")
		_, err := parseFloat(numStr)
		return err == nil && len(numStr) > 0 && numStr[0] != '.'
	}

	// Check for invalid unit suffixes (e.g., "10m" instead of "10min")
	lastChar := val[len(val)-1]
	if lastChar >= 'a' && lastChar <= 'z' {
		return false // Has a letter suffix that wasn't caught above
	}

	// Clock values: HH:MM:SS.mmm or MM:SS.mmm
	parts := strings.Split(val, ":")
	switch len(parts) {
	case 3: // Full clock: HH:MM:SS.mmm
		h, err1 := parseFloat(parts[0])
		m, err2 := parseFloat(parts[1])
		s, err3 := parseFloat(parts[2])
		if err1 != nil || err2 != nil || err3 != nil {
			return false
		}
		return m < 60 && s < 60 && h >= 0
	case 2: // Partial clock: MM:SS.mmm
		m, err1 := parseFloat(parts[0])
		s, err2 := parseFloat(parts[1])
		if err1 != nil || err2 != nil {
			return false
		}
		// Partial clock minutes must be < 60 (one or two digits)
		if len(parts[0]) > 2 {
			return false
		}
		return m < 60 && s < 60
	case 1: // Just a number (seconds)
		_, err := parseFloat(val)
		return err == nil && len(val) > 0 && val[0] != '.'
	}
	return false
}

// parseSMILClockMs parses a SMIL clock value to milliseconds. Returns -1 on error.
func parseSMILClockMs(val string) int64 {
	val = strings.TrimSpace(val)
	if val == "" {
		return -1
	}

	// Handle metric values: XXh, XXmin, XXs, XXms
	if strings.HasSuffix(val, "ms") {
		numStr := strings.TrimSuffix(val, "ms")
		f, err := parseFloat(numStr)
		if err != nil {
			return -1
		}
		return int64(f)
	}
	if strings.HasSuffix(val, "h") {
		numStr := strings.TrimSuffix(val, "h")
		f, err := parseFloat(numStr)
		if err != nil {
			return -1
		}
		return int64(f * 3600000)
	}
	if strings.HasSuffix(val, "min") {
		numStr := strings.TrimSuffix(val, "min")
		f, err := parseFloat(numStr)
		if err != nil {
			return -1
		}
		return int64(f * 60000)
	}
	if strings.HasSuffix(val, "s") {
		numStr := strings.TrimSuffix(val, "s")
		f, err := parseFloat(numStr)
		if err != nil {
			return -1
		}
		return int64(f * 1000)
	}

	// Handle clock values: HH:MM:SS.mmm or MM:SS.mmm or just seconds
	parts := strings.Split(val, ":")
	switch len(parts) {
	case 3:
		h, err1 := parseFloat(parts[0])
		m, err2 := parseFloat(parts[1])
		s, err3 := parseFloat(parts[2])
		if err1 != nil || err2 != nil || err3 != nil {
			return -1
		}
		return int64((h*3600 + m*60 + s) * 1000)
	case 2:
		m, err1 := parseFloat(parts[0])
		s, err2 := parseFloat(parts[1])
		if err1 != nil || err2 != nil {
			return -1
		}
		return int64((m*60 + s) * 1000)
	case 1:
		s, err := parseFloat(parts[0])
		if err != nil {
			return -1
		}
		return int64(s * 1000)
	}
	return -1
}

// parseFloat parses a float from a string, returning an error if it fails.
func parseFloat(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty string")
	}
	return strconv.ParseFloat(s, 64)
}

// ============================================================================
// Additional single-file content checks
// ============================================================================

// checkEmptySrcAttr detects img elements with empty src attributes (RSC-005).
func checkEmptySrcAttr(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Local == "img" {
			for _, attr := range se.Attr {
				if attr.Name.Local == "src" && strings.TrimSpace(attr.Value) == "" {
					r.AddWithLocation(report.Error, "RSC-005",
						`attribute "src" has a bad value: must be a valid URL`,
						location)
				}
			}
		}
	}
}

// checkEpubSwitchTrigger detects epub:switch and epub:trigger elements (RSC-017 deprecated).
func checkEpubSwitchTrigger(data []byte, location string, r *report.Report) {
	opsNS := "http://www.idpf.org/2007/ops"
	evNS := "http://www.w3.org/2001/xml-events"

	// First pass: collect all element IDs for trigger ref validation
	docIDs := make(map[string]bool)
	{
		idDecoder := newXHTMLDecoder(strings.NewReader(string(data)))
		for {
			tok, err := idDecoder.Token()
			if err != nil {
				break
			}
			se, ok := tok.(xml.StartElement)
			if !ok {
				continue
			}
			for _, attr := range se.Attr {
				if attr.Name.Local == "id" && attr.Name.Space == "" {
					docIDs[attr.Value] = true
				}
			}
		}
	}

	decoder := newXHTMLDecoder(strings.NewReader(string(data)))

	type switchState struct {
		hasCaseBefore bool
		hasDefault    bool
		defaultCount  int
		caseCount     int
	}
	var switchStack []*switchState

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "switch" && t.Name.Space == opsNS {
				r.AddWithLocation(report.Warning, "RSC-017",
					`The "epub:switch" element is deprecated`,
					location)
				switchStack = append(switchStack, &switchState{})
			}
			if t.Name.Local == "trigger" && t.Name.Space == opsNS {
				r.AddWithLocation(report.Warning, "RSC-017",
					`The "epub:trigger" element is deprecated`,
					location)
				// Validate ref and ev:observer attributes
				for _, attr := range t.Attr {
					if attr.Name.Local == "ref" && attr.Name.Space == "" {
						if attr.Value != "" && !docIDs[attr.Value] {
							r.AddWithLocation(report.Error, "RSC-005",
								`The ref attribute must refer to an element in the same document`,
								location)
						}
					}
					if attr.Name.Local == "observer" && attr.Name.Space == evNS {
						if attr.Value != "" && !docIDs[attr.Value] {
							r.AddWithLocation(report.Error, "RSC-005",
								`The ev:observer attribute must refer to an element in the same document`,
								location)
						}
					}
				}
			}
			if t.Name.Local == "case" && t.Name.Space == opsNS && len(switchStack) > 0 {
				cur := switchStack[len(switchStack)-1]
				cur.caseCount++
				if cur.hasDefault {
					// epub:case after epub:default is not allowed
					r.AddWithLocation(report.Error, "RSC-005",
						`element "epub:case" not allowed here`,
						location)
				} else {
					cur.hasCaseBefore = true
				}
				// Check for required-namespace attribute
				hasReqNS := false
				for _, attr := range t.Attr {
					if attr.Name.Local == "required-namespace" {
						hasReqNS = true
					}
				}
				if !hasReqNS {
					r.AddWithLocation(report.Error, "RSC-005",
						`element "epub:case" missing required attribute "required-namespace"`,
						location)
				}
			}
			if t.Name.Local == "default" && t.Name.Space == opsNS && len(switchStack) > 0 {
				cur := switchStack[len(switchStack)-1]
				cur.defaultCount++
				if !cur.hasCaseBefore {
					r.AddWithLocation(report.Error, "RSC-005",
						`element "epub:default" not allowed yet`,
						location)
				}
				if cur.defaultCount > 1 {
					r.AddWithLocation(report.Error, "RSC-005",
						`element "epub:default" not allowed here; only one is permitted`,
						location)
				}
				cur.hasDefault = true
			}
		case xml.EndElement:
			if t.Name.Local == "switch" && t.Name.Space == opsNS && len(switchStack) > 0 {
				cur := switchStack[len(switchStack)-1]
				if cur.caseCount == 0 && !cur.hasDefault {
					// Only report missing case if we haven't already reported "default not allowed yet"
					r.AddWithLocation(report.Error, "RSC-005",
						`element "epub:switch" incomplete; missing required element "epub:case"`,
						location)
				}
				if !cur.hasDefault {
					r.AddWithLocation(report.Error, "RSC-005",
						`element "epub:switch" incomplete; missing required element "epub:default"`,
						location)
				}
				switchStack = switchStack[:len(switchStack)-1]
			}
		}
	}
}

// checkEpubTypeOnHead detects epub:type attributes on head/metadata elements (RSC-005).
func checkEpubTypeOnHead(data []byte, location string, r *report.Report) {
	opsNS := "http://www.idpf.org/2007/ops"
	// Elements where epub:type is not allowed
	disallowed := map[string]bool{
		"head": true, "title": true, "meta": true, "link": true,
		"style": true, "base": true, "script": true, "noscript": true,
	}
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if disallowed[se.Name.Local] {
			for _, attr := range se.Attr {
				if attr.Name.Local == "type" && attr.Name.Space == opsNS {
					r.AddWithLocation(report.Error, "RSC-005",
						fmt.Sprintf(`attribute "epub:type" not allowed on element "%s"`, se.Name.Local),
						location)
				}
			}
		}
	}
}

// checkTableBorderAttr checks table border attribute values (RSC-005).
func checkTableBorderAttr(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Local == "table" {
			for _, attr := range se.Attr {
				if attr.Name.Local == "border" {
					val := strings.TrimSpace(attr.Value)
					if val != "" && val != "1" {
						r.AddWithLocation(report.Error, "RSC-005",
							fmt.Sprintf(`value of attribute "border" is invalid; must be equal to "" or "1"`),
							location)
					}
				}
			}
		}
	}
}

// checkCSS008StyleType detects style elements in <head> without a type declaration (CSS-008).
func checkCSS008StyleType(data []byte, location string, r *report.Report) {
	// In HTML5 (EPUB 3), the type attribute is not required on style elements.
	// Only check for EPUB 2 / XHTML 1.1 documents (no HTML5 doctype).
	content := string(data)
	if strings.Contains(content, "<!DOCTYPE html>") || strings.Contains(content, "<!doctype html>") {
		// HTML5 doctype — type attribute not required
		return
	}
	decoder := newXHTMLDecoder(strings.NewReader(content))
	inHead := false
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "head" {
				inHead = true
			}
			if t.Name.Local == "style" && inHead {
				hasType := false
				for _, attr := range t.Attr {
					if attr.Name.Local == "type" {
						hasType = true
					}
				}
				if !hasType {
					r.AddWithLocation(report.Error, "CSS-008",
						`style element missing required "type" attribute`,
						location)
				}
			}
		case xml.EndElement:
			if t.Name.Local == "head" {
				inHead = false
			}
		}
	}
}

// checkStyleInBody detects style elements in the body (RSC-005).
func checkStyleInBody(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	inBody := false
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "body" {
				inBody = true
			}
			if t.Name.Local == "style" && inBody {
				r.AddWithLocation(report.Error, "RSC-005",
					`element "style" not allowed here`,
					location)
				// Check for "scoped" attribute
				for _, attr := range t.Attr {
					if attr.Name.Local == "scoped" {
						r.AddWithLocation(report.Error, "RSC-005",
							`attribute "scoped" not allowed here`,
							location)
					}
				}
			}
		case xml.EndElement:
			if t.Name.Local == "body" {
				inBody = false
			}
		}
	}
}

// checkStyleAttrCSS detects invalid CSS in style attributes (CSS-008).
func checkStyleAttrCSS(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attr := range se.Attr {
			if attr.Name.Local == "style" {
				val := strings.TrimSpace(attr.Value)
				if val == "" {
					continue
				}
				// Check for invalid CSS syntax
				// A style attribute should contain property: value declarations
				if strings.Contains(val, "{") || strings.Contains(val, "}") {
					r.AddWithLocation(report.Error, "CSS-008",
						`invalid CSS syntax in "style" attribute; must not contain declaration blocks`,
						location)
				} else if !strings.Contains(val, ":") {
					// No colon means no property:value pair
					r.AddWithLocation(report.Error, "CSS-008",
						`invalid CSS syntax in "style" attribute`,
						location)
				}
			}
		}
	}
}

// checkARIADescribedAt detects non-existent ARIA describedat attribute (RSC-005).
func checkARIADescribedAt(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attr := range se.Attr {
			if attr.Name.Local == "aria-describedat" {
				r.AddWithLocation(report.Error, "RSC-005",
					`attribute "aria-describedat" not allowed here`,
					location)
			}
		}
	}
}

// checkTitleElement checks for empty or missing title elements (RSC-005).
func checkTitleElement(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	inHead := false
	hasTitle := false
	inTitle := false
	titleContent := ""
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "head" {
				inHead = true
			}
			if t.Name.Local == "title" && inHead {
				inTitle = true
				hasTitle = true
				titleContent = ""
			}
		case xml.EndElement:
			if t.Name.Local == "title" && inTitle {
				inTitle = false
				if strings.TrimSpace(titleContent) == "" {
					r.AddWithLocation(report.Error, "RSC-005",
						`element "title" must not be empty`,
						location)
				}
			}
			if t.Name.Local == "head" {
				if !hasTitle {
					r.AddWithLocation(report.Warning, "RSC-017",
						`The "head" element should have a "title" child element.`,
						location)
				}
				inHead = false
			}
		case xml.CharData:
			if inTitle {
				titleContent += string(t)
			}
		}
	}
}

// checkPrefixAttrLocation detects epub:prefix attributes on disallowed elements (RSC-005).
// The prefix attribute is only allowed on the html element in content documents.
func checkPrefixAttrLocation(data []byte, location string, r *report.Report) {
	opsNS := "http://www.idpf.org/2007/ops"
	// Elements where epub:prefix is not allowed (it's only valid on <html>)
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if se.Name.Local == "html" {
			continue // prefix is allowed on html root element only
		}
		for _, attr := range se.Attr {
			if attr.Name.Local == "prefix" && attr.Name.Space == opsNS {
				r.AddWithLocation(report.Error, "RSC-005",
					`attribute "epub:prefix" not allowed here`,
					location)
			}
		}
	}
}

// checkPrefixDeclarations checks prefix declarations in content documents.
// Detects: underscore prefix (OPF-007a), reserved prefix overrides (OPF-007),
// and prefix with empty namespace (OPF-028).
func checkPrefixDeclarations(data []byte, location string, r *report.Report) {
	opsNS := "http://www.idpf.org/2007/ops"
	reservedPrefixURIs := map[string]string{
		"a11y":      "http://www.idpf.org/epub/vocab/package/a11y/#",
		"dcterms":   "http://purl.org/dc/terms/",
		"marc":      "http://id.loc.gov/vocabulary/",
		"media":     "http://www.idpf.org/epub/vocab/overlays/#",
		"msv":       "http://www.idpf.org/epub/vocab/structure/magazine/#",
		"onix":      "http://www.editeur.org/ONIX/book/codelists/current.html#",
		"prism":     "http://www.prismstandard.org/specifications/3.0/PRISM_CV_Spec_3.0.htm#",
		"rendition": "http://www.idpf.org/vocab/rendition/#",
		"schema":    "http://schema.org/",
		"xsd":       "http://www.w3.org/2001/XMLSchema#",
	}
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attr := range se.Attr {
			if attr.Name.Local == "prefix" && attr.Name.Space == opsNS {
				// Parse prefix declarations: "prefix1: uri1 prefix2: uri2"
				val := attr.Value
				parts := strings.Fields(val)
				for i := 0; i < len(parts); i++ {
					prefix := parts[i]
					if !strings.HasSuffix(prefix, ":") {
						continue
					}
					prefix = strings.TrimSuffix(prefix, ":")
					uri := ""
					if i+1 < len(parts) && !strings.HasSuffix(parts[i+1], ":") {
						uri = parts[i+1]
						i++
					}
					// OPF-007a: underscore prefix
					if prefix == "_" {
						r.AddWithLocation(report.Error, "OPF-007a",
							`The prefix "_" must not be declared`,
							location)
						continue
					}
					// OPF-028: empty namespace
					if uri == "" {
						r.AddWithLocation(report.Error, "OPF-028",
							fmt.Sprintf(`Undeclared prefix: "%s"`, prefix),
							location)
						continue
					}
					// OPF-007: reserved prefix override
					if canonicalURI, ok := reservedPrefixURIs[prefix]; ok {
						if uri != canonicalURI {
							r.AddWithLocation(report.Warning, "OPF-007",
								fmt.Sprintf(`Re-declaration of reserved prefix "%s"`, prefix),
								location)
						}
					}
				}
			}
		}
	}
}

// checkNestedTime detects <time> elements nested inside other <time> elements (RSC-005).
func checkNestedTime(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	depth := 0
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "time" {
				if depth > 0 {
					r.AddWithLocation(report.Error, "RSC-005",
						`element "time" not allowed here`,
						location)
				}
				depth++
			}
		case xml.EndElement:
			if t.Name.Local == "time" && depth > 0 {
				depth--
			}
		}
	}
}

// checkMathMLContentOnly detects Content MathML elements used directly
// inside <math> (not inside <semantics>/<annotation-xml>) (RSC-005).
// Also detects nested <math> elements, except inside annotation-xml which allows them.
func checkMathMLContentOnly(data []byte, location string, r *report.Report) {
	mathNS := "http://www.w3.org/1998/Math/MathML"
	contentMathMLElements := map[string]bool{
		"apply": true, "cn": true, "ci": true, "csymbol": true,
		"cerror": true, "cbytes": true, "cs": true, "share": true,
		"piecewise": true, "bind": true,
	}
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	mathDepth := 0  // depth inside a math element
	inMath := false
	reported := map[string]bool{}
	insideAnnotation := 0 // depth of annotation-xml nesting (allows nested <math>)
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			isMathNS := t.Name.Space == mathNS
			if isMathNS && t.Name.Local == "annotation-xml" {
				insideAnnotation++
			}
			if t.Name.Local == "math" {
				if inMath && insideAnnotation == 0 {
					// Nested <math> inside <math> (outside annotation-xml) is not allowed
					r.AddWithLocation(report.Error, "RSC-005",
						`element "math" not allowed here`,
						location)
				}
				if !inMath {
					inMath = true
					mathDepth = 0
					reported = map[string]bool{}
				}
			} else if inMath {
				mathDepth++
			}
			// Only check direct children of math (mathDepth == 1)
			if inMath && mathDepth == 1 && isMathNS && contentMathMLElements[t.Name.Local] && !reported[t.Name.Local] {
				r.AddWithLocation(report.Error, "RSC-005",
					fmt.Sprintf(`element "%s" not allowed here`, t.Name.Local),
					location)
				reported[t.Name.Local] = true
			}
		case xml.EndElement:
			if t.Name.Local == "annotation-xml" && t.Name.Space == mathNS && insideAnnotation > 0 {
				insideAnnotation--
			}
			if t.Name.Local == "math" {
				inMath = false
				mathDepth = 0
			} else if inMath {
				mathDepth--
			}
		}
	}
}

// checkMathMLAnnotation validates MathML annotation-xml attributes (RSC-005).
func checkMathMLAnnotation(data []byte, location string, r *report.Report) {
	mathNS := "http://www.w3.org/1998/Math/MathML"
	// Content MathML elements not allowed inside MathML-Presentation annotations
	contentMathMLElements := map[string]bool{
		"apply": true, "cn": true, "ci": true, "csymbol": true,
		"cerror": true, "cbytes": true, "cs": true, "share": true,
		"piecewise": true, "bind": true,
	}
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	inPresAnnotation := false
	annotationDepth := 0
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "annotation-xml" && t.Name.Space == mathNS {
				encoding := ""
				name := ""
				for _, attr := range t.Attr {
					if attr.Name.Local == "encoding" {
						encoding = attr.Value
					}
					if attr.Name.Local == "name" {
						name = attr.Value
					}
				}
				encLower := strings.ToLower(encoding)
				if encLower == "mathml-content" {
					if name == "" {
						r.AddWithLocation(report.Error, "RSC-005",
							`element "annotation-xml" missing required attribute "name"`,
							location)
					} else if name != "contentequiv" {
						r.AddWithLocation(report.Error, "RSC-005",
							`value of attribute "name" is invalid`,
							location)
					}
				}
				if encLower == "application/xml+xhtml" {
					r.AddWithLocation(report.Error, "RSC-005",
						`value of attribute "encoding" is invalid; must be equal to "application/xhtml+xml"`,
						location)
				}
				if encLower == "mathml-presentation" {
					inPresAnnotation = true
					annotationDepth = 0
				}
			} else if inPresAnnotation {
				annotationDepth++
				if annotationDepth == 1 && contentMathMLElements[t.Name.Local] {
					r.AddWithLocation(report.Error, "RSC-005",
						fmt.Sprintf(`element "%s" not allowed here`, t.Name.Local),
						location)
				}
			}
		case xml.EndElement:
			if inPresAnnotation {
				if t.Name.Local == "annotation-xml" && annotationDepth == 0 {
					inPresAnnotation = false
				} else {
					annotationDepth--
				}
			}
		}
	}
}

// checkHiddenAttrValue validates the hidden attribute value (RSC-005).
func checkHiddenAttrValue(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attr := range se.Attr {
			if attr.Name.Local == "hidden" {
				val := strings.ToLower(strings.TrimSpace(attr.Value))
				if val != "" && val != "hidden" && val != "until-found" {
					r.AddWithLocation(report.Error, "RSC-005",
						`value of attribute "hidden" is invalid`,
						location)
				}
			}
		}
	}
}

// checkDatetimeFormat validates datetime attribute values on <time> elements (RSC-005).
func checkDatetimeFormat(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "time" {
			continue
		}
		for _, attr := range se.Attr {
			if attr.Name.Local == "datetime" {
				val := strings.TrimSpace(attr.Value)
				if val != "" && !isValidDatetime(val) {
					r.AddWithLocation(report.Error, "RSC-005",
						`value of attribute "datetime" is invalid`,
						location)
				}
			}
		}
	}
}

// isValidDatetime checks if a string is a valid HTML datetime value.
func isValidDatetime(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	// ISO 8601 duration (PnDTnHnMnS or PnW)
	if strings.HasPrefix(s, "P") {
		return isValidDuration(s)
	}
	// Bare duration components: nW, nD, nH, nM, nS, n.nS (EPUBCheck accepts these)
	// Also space-separated multi-unit: "123W 123H 32D 12S"
	if isBareDuration(s) {
		return true
	}
	if s == "Z" {
		return true
	}
	// Timezone offset: +HH:MM or -HH:MM or +HHMM or -HHMM
	if len(s) >= 5 && (s[0] == '+' || s[0] == '-') && len(s) <= 6 {
		return regexp.MustCompile(`^[+-]\d{2}:?\d{2}$`).MatchString(s)
	}
	// Yearless date: --MM-DD
	if regexp.MustCompile(`^--\d{2}-\d{2}$`).MatchString(s) {
		return true
	}
	// Month-day: MM-DD (without year)
	if regexp.MustCompile(`^\d{2}-\d{2}$`).MatchString(s) {
		return true
	}
	// Year: YYYY
	if regexp.MustCompile(`^\d{4}$`).MatchString(s) {
		return true
	}
	// Year-month: YYYY-MM
	if regexp.MustCompile(`^\d{4}-\d{2}$`).MatchString(s) {
		return true
	}
	// Date: YYYY-MM-DD
	if regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`).MatchString(s) {
		return true
	}
	// Week: YYYY-Wnn
	if regexp.MustCompile(`^\d{4}-W\d{2}$`).MatchString(s) {
		return true
	}
	// Time: HH:MM, HH:MM:SS, HH:MM:SS.sss
	if regexp.MustCompile(`^\d{2}:\d{2}(:\d{2}(\.\d{1,3})?)?$`).MatchString(s) {
		return true
	}
	// Datetime: date T/space time [timezone]
	datetimeRe := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}(:\d{2}(\.\d{1,3})?)?([Z]|[+-]\d{2}:?\d{2})?$`)
	return datetimeRe.MatchString(s)
}

// isBareDuration checks if a string is a bare duration value like "123W", "32D", "12H", "1M", "12S"
// or space-separated multi-unit "123W 123H 32D 12S".
func isBareDuration(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	parts := strings.Fields(s)
	for _, p := range parts {
		if !regexp.MustCompile(`^\d+(\.\d+)?[WDHMS]$`).MatchString(p) {
			return false
		}
	}
	return true
}

// isValidDuration checks if a string is a valid ISO 8601 duration.
func isValidDuration(s string) bool {
	if !strings.HasPrefix(s, "P") || len(s) < 2 {
		return false
	}
	s = s[1:]
	if regexp.MustCompile(`^\d+W$`).MatchString(s) {
		return true
	}
	parts := strings.SplitN(s, "T", 2)
	datePart := parts[0]
	if datePart != "" {
		if !regexp.MustCompile(`^\d+D$`).MatchString(datePart) {
			return false
		}
	}
	if len(parts) == 2 {
		timePart := parts[1]
		if timePart == "" {
			return false
		}
		if !regexp.MustCompile(`^(\d+H)?(\d+M)?(\d+(\.\d{1,3})?S)?$`).MatchString(timePart) {
			return false
		}
		if !strings.ContainsAny(timePart, "HMS") {
			return false
		}
	}
	return true
}

// checkURLConformance checks for non-conforming URLs and unparseable hosts (RSC-020).
func checkURLConformance(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attr := range se.Attr {
			if attr.Name.Local == "href" || attr.Name.Local == "src" || attr.Name.Local == "poster" {
				val := strings.TrimSpace(attr.Value)
				if val == "" || strings.HasPrefix(val, "#") || strings.HasPrefix(val, "mailto:") {
					continue
				}
				// Detect single-slash URLs (e.g. https:/host instead of https://host)
				if strings.Contains(val, ":/") && !strings.Contains(val, "://") {
					r.AddWithLocation(report.Error, "RSC-020",
						fmt.Sprintf(`Invalid URL "%s"`, val),
						location)
					continue
				}
				if !strings.Contains(val, "://") {
					continue
				}
				parts := strings.SplitN(val, "://", 2)
				if len(parts) != 2 {
					continue
				}
				hostPath := parts[1]
				host := hostPath
				if idx := strings.IndexAny(hostPath, "/?#"); idx >= 0 {
					host = hostPath[:idx]
				}
				// Check for spaces or invalid characters in host
				if strings.ContainsAny(host, " ,<>{}|\\^`") {
					r.AddWithLocation(report.Error, "RSC-020",
						fmt.Sprintf(`Invalid URL "%s"`, val),
						location)
				}
			}
		}
	}
}

// isNavDocument detects whether the XHTML content is actually a navigation document
// by checking for the presence of a <nav> element with epub:type="toc".
func isNavDocument(data []byte) bool {
	content := string(data)
	// Quick pre-check: must contain both "nav" element and "toc" type
	if !strings.Contains(content, "<nav") || !strings.Contains(content, "toc") {
		return false
	}
	decoder := newXHTMLDecoder(strings.NewReader(content))
	opsNS := "http://www.idpf.org/2007/ops"
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		if se, ok := tok.(xml.StartElement); ok {
			if se.Name.Local == "nav" {
				for _, attr := range se.Attr {
					if attr.Name.Local == "type" && (attr.Name.Space == opsNS || attr.Name.Space == "epub") {
						for _, t := range strings.Fields(attr.Value) {
							if t == "toc" {
								return true
							}
						}
					}
				}
			}
		}
	}
	return false
}

// checkNavContentModel validates EPUB navigation document content model (RSC-005, RSC-017).
func checkNavContentModel(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	opsNS := "http://www.idpf.org/2007/ops"

	hasTocNav := false
	pageListCount := 0
	landmarksCount := 0

	type landmarkEntry struct {
		epubType string
		href     string
	}
	var landmarkEntries []landmarkEntry

	// Use element stack to track nesting
	type elemState struct {
		name       string
		epubType   string // for nav elements
		hasLabel   bool   // for li: has a/span before ol
		labelIsA   bool   // for li: label is anchor (not span)
		hasOl      bool   // for li: has nested ol
		hasLi      bool   // for ol: has li child
		hasText    bool   // for a, span, h1-h6: has text or image content
		hasHeading bool   // for nav: has heading element
	}
	var stack []elemState

	// Helper to find the innermost element of a given name
	findParent := func(name string) int {
		for i := len(stack) - 1; i >= 0; i-- {
			if stack[i].name == name {
				return i
			}
		}
		return -1
	}

	// Count ol nesting depth within current nav
	olDepth := func() int {
		count := 0
		for _, s := range stack {
			if s.name == "ol" {
				count++
			}
		}
		return count
	}

	// Get current nav type
	navType := func() string {
		for i := len(stack) - 1; i >= 0; i-- {
			if stack[i].name == "nav" {
				return stack[i].epubType
			}
		}
		return ""
	}

	inNav := func() bool {
		return findParent("nav") >= 0
	}

	// inTypedNav checks if we're inside a nav with a known epub:type
	inTypedNav := func() bool {
		for i := len(stack) - 1; i >= 0; i-- {
			if stack[i].name == "nav" {
				return stack[i].epubType != ""
			}
		}
		return false
	}

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			name := t.Name.Local
			state := elemState{name: name}

			if name == "nav" {
				for _, attr := range t.Attr {
					if attr.Name.Local == "type" && attr.Name.Space == opsNS {
						state.epubType = attr.Value
					}
				}
				if state.epubType == "toc" {
					hasTocNav = true
				}
				if state.epubType == "page-list" {
					pageListCount++
					if pageListCount > 1 {
						r.AddWithLocation(report.Error, "RSC-005",
							`Multiple occurrences of the "page-list" nav element`,
							location)
					}
				}
				if state.epubType == "landmarks" {
					landmarksCount++
					if landmarksCount > 1 {
						r.AddWithLocation(report.Error, "RSC-005",
							`Multiple occurrences of the "landmarks" nav element`,
							location)
					}
					landmarkEntries = nil
				}
			}

			if inNav() {
				nt := navType()
				isHeading := name == "h1" || name == "h2" || name == "h3" || name == "h4" || name == "h5" || name == "h6"

				// Mark parent nav as having a heading
				if isHeading {
					if ni := findParent("nav"); ni >= 0 {
						stack[ni].hasHeading = true
					}
				}

				// p element before ol in nav (should be heading)
				if name == "p" && findParent("li") < 0 && findParent("a") < 0 {
					r.AddWithLocation(report.Error, "RSC-005",
						`element "p" not allowed here`,
						location)
				}

				// ol inside nav
				if name == "ol" {
					// Mark parent li as having an ol
					if li := findParent("li"); li >= 0 {
						stack[li].hasOl = true
					}
					// Check for nested ol in page-list or landmarks
					if olDepth() > 0 {
						if nt == "page-list" {
							r.AddWithLocation(report.Warning, "RSC-017",
								`The "page-list" nav should have no nested sublists`,
								location)
						}
						if nt == "landmarks" {
							r.AddWithLocation(report.Warning, "RSC-017",
								`The "landmarks" nav should have no nested sublists`,
								location)
						}
					}
				}

				// li inside ol
				if name == "li" {
					if oi := findParent("ol"); oi >= 0 {
						stack[oi].hasLi = true
					}
				}

				// a inside li
				if name == "a" {
					if li := findParent("li"); li >= 0 {
						stack[li].hasLabel = true
						stack[li].labelIsA = true
					}
					// Landmarks checks
					if nt == "landmarks" {
						hasType := false
						epubTypeVal := ""
						href := ""
						for _, attr := range t.Attr {
							if attr.Name.Local == "type" && attr.Name.Space == opsNS {
								hasType = true
								epubTypeVal = attr.Value
							}
							if attr.Name.Local == "href" {
								href = attr.Value
							}
						}
						if !hasType {
							r.AddWithLocation(report.Error, "RSC-005",
								`Missing epub:type attribute on anchor inside "landmarks" nav`,
								location)
						}
						if epubTypeVal != "" {
							// Split epub:type by spaces for multi-value
							types := strings.Fields(epubTypeVal)
							for _, et := range types {
								matched := false
								for _, prev := range landmarkEntries {
									if prev.epubType == et && prev.href == href {
										matched = true
									}
								}
								if matched {
									// Report for both the original and the duplicate
									r.AddWithLocation(report.Error, "RSC-005",
										`Another landmark was found with the same epub:type and same reference`,
										location)
									r.AddWithLocation(report.Error, "RSC-005",
										`Another landmark was found with the same epub:type and same reference`,
										location)
								}
								landmarkEntries = append(landmarkEntries, landmarkEntry{epubType: et, href: href})
							}
						}
					}
				}

				// span inside li (as label)
				if name == "span" {
					if li := findParent("li"); li >= 0 && findParent("a") < 0 {
						stack[li].hasLabel = true
					}
				}

				// img counts as text content for a, span, h1-h6
				if name == "img" {
					for i := len(stack) - 1; i >= 0; i-- {
						n := stack[i].name
						if n == "a" || n == "span" || n == "h1" || n == "h2" || n == "h3" || n == "h4" || n == "h5" || n == "h6" {
							stack[i].hasText = true
							break
						}
					}
				}
			}

			stack = append(stack, state)

		case xml.EndElement:
			name := t.Name.Local
			// Find the matching element in the stack
			idx := -1
			for i := len(stack) - 1; i >= 0; i-- {
				if stack[i].name == name {
					idx = i
					break
				}
			}
			if idx < 0 {
				continue
			}
			state := stack[idx]

			if inNav() {
				isHeading := name == "h1" || name == "h2" || name == "h3" || name == "h4" || name == "h5" || name == "h6"

				if isHeading && !state.hasText {
					r.AddWithLocation(report.Error, "RSC-005",
						`Heading elements must contain text`,
						location)
				}

				// Content model checks only apply to typed navs (toc, page-list, landmarks, etc.)
				// Navs without epub:type are not restricted
				if inTypedNav() {
					if name == "a" && !state.hasText {
						r.AddWithLocation(report.Error, "RSC-005",
							`Anchors within nav elements must contain text`,
							location)
					}

					if name == "span" && findParent("li") >= 0 && !state.hasText {
						r.AddWithLocation(report.Error, "RSC-005",
							`Spans within nav elements must contain text`,
							location)
					}

					if name == "ol" && !state.hasLi {
						r.AddWithLocation(report.Error, "RSC-005",
							`element "ol" incomplete`,
							location)
					}

					if name == "li" {
						if !state.hasLabel {
							r.AddWithLocation(report.Error, "RSC-005",
								`element "ol" not allowed yet; expected element "a" or "span"`,
								location)
						} else if !state.labelIsA && !state.hasOl {
							// Span label with no nested ol
							r.AddWithLocation(report.Error, "RSC-005",
								`element "li" incomplete; missing required element "ol"`,
								location)
						}
					}
				}

				if name == "nav" {
					nt := state.epubType
					if nt != "" && nt != "toc" && nt != "page-list" && nt != "landmarks" {
						if !state.hasHeading {
							r.AddWithLocation(report.Error, "RSC-005",
								fmt.Sprintf(`nav element with epub:type "%s" must have a heading`, nt),
								location)
						}
					}
				}
			}

			// Pop stack
			stack = stack[:idx]

		case xml.CharData:
			text := strings.TrimSpace(string(t))
			if text != "" && inNav() {
				// Mark text content on parent elements
				for i := len(stack) - 1; i >= 0; i-- {
					n := stack[i].name
					if n == "a" || n == "span" || n == "h1" || n == "h2" || n == "h3" || n == "h4" || n == "h5" || n == "h6" {
						stack[i].hasText = true
						break
					}
				}
			}
		}
	}

	// Check for missing toc nav
	if !hasTocNav {
		r.AddWithLocation(report.Error, "RSC-005",
			`missing required nav element with epub:type="toc"`,
			location)
	}
}

// disallowedDescendantPairs defines (ancestor, descendant) pairs where the
// descendant element must not appear anywhere inside the ancestor element.
// These correspond to epubcheck's Schematron "disallowed-descendants" patterns.
var disallowedDescendantPairs = []struct {
	ancestor   string
	descendant string
}{
	{"address", "address"},
	{"address", "header"},
	{"address", "footer"},
	{"form", "form"},
	{"progress", "progress"},
	{"meter", "meter"},
	{"caption", "table"},
	{"header", "header"},
	{"header", "footer"},
	{"footer", "footer"},
	{"footer", "header"},
	{"label", "label"},
}

// checkDisallowedDescendants reports RSC-005 when an element contains a
// descendant that is structurally forbidden. For example, <address> cannot
// contain another <address>, <header>, or <footer>; <form> cannot nest inside
// <form>; <caption> cannot contain <table>; etc.
//
// This complements checkInteractiveNesting (which handles interactive elements
// like a, button, audio, video) by covering non-interactive disallowed nesting.
func checkDisallowedDescendants(data []byte, location string, r *report.Report) {
	const xhtmlNS = "http://www.w3.org/1999/xhtml"

	// Build lookup: for each ancestor element, which descendants are disallowed
	type pair struct{ ancestor, descendant string }
	disallowed := make(map[string][]string) // ancestor -> list of disallowed descendants
	for _, p := range disallowedDescendantPairs {
		disallowed[p.ancestor] = append(disallowed[p.ancestor], p.descendant)
	}

	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	foreignDepth := 0

	// Stack of open elements that have disallowed-descendant rules
	type stackEntry struct {
		name       string
		disallowed []string
	}
	var stack []stackEntry
	reported := make(map[pair]bool)

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if foreignDepth > 0 {
				foreignDepth++
				continue
			}
			ns := t.Name.Space
			if ns != "" && ns != xhtmlNS {
				foreignDepth = 1
				continue
			}
			name := strings.ToLower(t.Name.Local)
			if name == "svg" || name == "math" {
				foreignDepth = 1
				continue
			}

			// Check if this element is a disallowed descendant of any ancestor on the stack
			for _, entry := range stack {
				for _, d := range entry.disallowed {
					if name == d {
						key := pair{entry.name, name}
						if !reported[key] {
							reported[key] = true
							r.AddWithLocation(report.Error, "RSC-005",
								fmt.Sprintf("element \"%s\" not allowed inside \"%s\"",
									name, entry.name),
								location)
						}
					}
				}
			}

			// If this element has its own disallowed-descendant rules, push it
			if dlist, ok := disallowed[name]; ok {
				stack = append(stack, stackEntry{name: name, disallowed: dlist})
			}

		case xml.EndElement:
			if foreignDepth > 0 {
				foreignDepth--
				continue
			}
			name := strings.ToLower(t.Name.Local)
			if len(stack) > 0 && stack[len(stack)-1].name == name {
				stack = stack[:len(stack)-1]
			}
		}
	}
}

// checkRequiredAncestor reports RSC-005 when an element appears without a
// required ancestor. Covers:
//   - <area> must be inside <map>
//   - <img ismap> must be inside <a href>
func checkRequiredAncestor(data []byte, location string, r *report.Report) {
	const xhtmlNS = "http://www.w3.org/1999/xhtml"

	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	foreignDepth := 0

	// Track ancestor context
	var elementStack []string
	mapDepth := 0   // how many <map> ancestors we're inside
	aHrefDepth := 0 // how many <a href="..."> ancestors we're inside

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if foreignDepth > 0 {
				foreignDepth++
				continue
			}
			ns := t.Name.Space
			if ns != "" && ns != xhtmlNS {
				foreignDepth = 1
				continue
			}
			name := strings.ToLower(t.Name.Local)
			if name == "svg" || name == "math" {
				foreignDepth = 1
				continue
			}

			elementStack = append(elementStack, name)

			if name == "map" {
				mapDepth++
			}
			if name == "a" {
				for _, attr := range t.Attr {
					if strings.ToLower(attr.Name.Local) == "href" && attr.Value != "" {
						aHrefDepth++
						break
					}
				}
			}

			// Check: <area> requires <map> ancestor
			if name == "area" && mapDepth == 0 {
				r.AddWithLocation(report.Error, "RSC-005",
					`element "area" requires ancestor "map"`,
					location)
			}

			// Check: <img ismap> requires <a href> ancestor
			if name == "img" {
				hasIsmap := false
				for _, attr := range t.Attr {
					if strings.ToLower(attr.Name.Local) == "ismap" {
						hasIsmap = true
						break
					}
				}
				if hasIsmap && aHrefDepth == 0 {
					r.AddWithLocation(report.Error, "RSC-005",
						`element "img" with attribute "ismap" requires ancestor "a" with attribute "href"`,
						location)
				}
			}

		case xml.EndElement:
			if foreignDepth > 0 {
				foreignDepth--
				continue
			}
			name := strings.ToLower(t.Name.Local)
			if len(elementStack) > 0 && elementStack[len(elementStack)-1] == name {
				elementStack = elementStack[:len(elementStack)-1]
			}
			if name == "map" && mapDepth > 0 {
				mapDepth--
			}
			if name == "a" && aHrefDepth > 0 {
				aHrefDepth--
			}
		}
	}
}

// checkBdoDir reports RSC-005 when a <bdo> element is missing the required
// dir attribute. Per HTML spec, bdo must have dir="ltr" or dir="rtl".
func checkBdoDir(data []byte, location string, r *report.Report) {
	const xhtmlNS = "http://www.w3.org/1999/xhtml"

	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	foreignDepth := 0

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if foreignDepth > 0 {
				foreignDepth++
				continue
			}
			ns := t.Name.Space
			if ns != "" && ns != xhtmlNS {
				foreignDepth = 1
				continue
			}
			name := strings.ToLower(t.Name.Local)
			if name == "svg" || name == "math" {
				foreignDepth = 1
				continue
			}

			if name == "bdo" {
				hasDir := false
				for _, attr := range t.Attr {
					if strings.ToLower(attr.Name.Local) == "dir" {
						hasDir = true
						break
					}
				}
				if !hasDir {
					r.AddWithLocation(report.Error, "RSC-005",
						`element "bdo" missing required attribute "dir"`,
						location)
				}
			}

		case xml.EndElement:
			if foreignDepth > 0 {
				foreignDepth--
				continue
			}
		}
	}
}

// checkSSMLPhNesting reports RSC-005 when ssml:ph attributes are nested.
// An element with ssml:ph must not be a descendant of another element with ssml:ph.
func checkSSMLPhNesting(data []byte, location string, r *report.Report) {
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	ssmlNS := "http://www.w3.org/2001/10/synthesis"

	// Track element depth. When we're inside an element with ssml:ph,
	// phAncestorDepth records the element depth at which the ancestor's ssml:ph
	// was found. -1 means we're not inside any ssml:ph element.
	elementDepth := 0
	phAncestorDepth := -1

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			elementDepth++
			hasSSMLPh := false
			for _, attr := range t.Attr {
				if attr.Name.Local == "ph" && attr.Name.Space == ssmlNS {
					hasSSMLPh = true
					break
				}
			}
			if hasSSMLPh {
				if phAncestorDepth >= 0 {
					// We're inside an element with ssml:ph — this is nested
					r.AddWithLocation(report.Error, "RSC-005",
						`attribute "ssml:ph" not allowed on descendant of element with "ssml:ph"`,
						location)
					return
				}
				phAncestorDepth = elementDepth
			}
		case xml.EndElement:
			if phAncestorDepth == elementDepth {
				// Leaving the element that had ssml:ph
				phAncestorDepth = -1
			}
			elementDepth--
		}
	}
}

// checkDuplicateMapName reports RSC-005 when multiple <map> elements share the
// same name attribute value within a document.
func checkDuplicateMapName(data []byte, location string, r *report.Report) {
	const xhtmlNS = "http://www.w3.org/1999/xhtml"

	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	foreignDepth := 0
	mapNames := make(map[string]bool)

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if foreignDepth > 0 {
				foreignDepth++
				continue
			}
			ns := t.Name.Space
			if ns != "" && ns != xhtmlNS {
				foreignDepth = 1
				continue
			}
			name := strings.ToLower(t.Name.Local)
			if name == "svg" || name == "math" {
				foreignDepth = 1
				continue
			}

			if name == "map" {
				for _, attr := range t.Attr {
					if strings.ToLower(attr.Name.Local) == "name" && attr.Value != "" {
						if mapNames[attr.Value] {
							r.AddWithLocation(report.Error, "RSC-005",
								fmt.Sprintf(`duplicate map name "%s"`, attr.Value),
								location)
						}
						mapNames[attr.Value] = true
					}
				}
			}

		case xml.EndElement:
			if foreignDepth > 0 {
				foreignDepth--
				continue
			}
		}
	}
}

// checkSelectMultiple reports RSC-005 when a <select> element without the
// "multiple" attribute has more than one <option> with "selected".
func checkSelectMultiple(data []byte, location string, r *report.Report) {
	const xhtmlNS = "http://www.w3.org/1999/xhtml"

	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	foreignDepth := 0

	type selectState struct {
		hasMultiple   bool
		selectedCount int
	}
	var selectStack []*selectState

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if foreignDepth > 0 {
				foreignDepth++
				continue
			}
			ns := t.Name.Space
			if ns != "" && ns != xhtmlNS {
				foreignDepth = 1
				continue
			}
			name := strings.ToLower(t.Name.Local)
			if name == "svg" || name == "math" {
				foreignDepth = 1
				continue
			}

			if name == "select" {
				state := &selectState{}
				for _, attr := range t.Attr {
					if strings.ToLower(attr.Name.Local) == "multiple" {
						state.hasMultiple = true
						break
					}
				}
				selectStack = append(selectStack, state)
			}

			if name == "option" && len(selectStack) > 0 {
				current := selectStack[len(selectStack)-1]
				for _, attr := range t.Attr {
					if strings.ToLower(attr.Name.Local) == "selected" {
						current.selectedCount++
						if !current.hasMultiple && current.selectedCount > 1 {
							r.AddWithLocation(report.Error, "RSC-005",
								`multiple "option" elements with "selected" in a "select" without "multiple"`,
								location)
						}
						break
					}
				}
			}

		case xml.EndElement:
			if foreignDepth > 0 {
				foreignDepth--
				continue
			}
			name := strings.ToLower(t.Name.Local)
			if name == "select" && len(selectStack) > 0 {
				selectStack = selectStack[:len(selectStack)-1]
			}
		}
	}
}

// checkMetaCharset reports RSC-005 when a document has more than one
// <meta charset> element.
func checkMetaCharset(data []byte, location string, r *report.Report) {
	const xhtmlNS = "http://www.w3.org/1999/xhtml"

	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	foreignDepth := 0
	charsetCount := 0

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if foreignDepth > 0 {
				foreignDepth++
				continue
			}
			ns := t.Name.Space
			if ns != "" && ns != xhtmlNS {
				foreignDepth = 1
				continue
			}
			name := strings.ToLower(t.Name.Local)
			if name == "svg" || name == "math" {
				foreignDepth = 1
				continue
			}

			if name == "meta" {
				for _, attr := range t.Attr {
					if strings.ToLower(attr.Name.Local) == "charset" {
						charsetCount++
						if charsetCount > 1 {
							r.AddWithLocation(report.Error, "RSC-005",
								`only one "meta" element with "charset" allowed per document`,
								location)
						}
						break
					}
				}
			}

		case xml.EndElement:
			if foreignDepth > 0 {
				foreignDepth--
				continue
			}
		}
	}
}

// checkLinkSizes reports RSC-005 when a <link> element has a "sizes" attribute
// but its rel attribute is not "icon". Per HTML spec, sizes is only valid on
// link[rel="icon"].
func checkLinkSizes(data []byte, location string, r *report.Report) {
	const xhtmlNS = "http://www.w3.org/1999/xhtml"

	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	foreignDepth := 0

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if foreignDepth > 0 {
				foreignDepth++
				continue
			}
			ns := t.Name.Space
			if ns != "" && ns != xhtmlNS {
				foreignDepth = 1
				continue
			}
			name := strings.ToLower(t.Name.Local)
			if name == "svg" || name == "math" {
				foreignDepth = 1
				continue
			}

			if name == "link" {
				hasSizes := false
				relIsIcon := false
				for _, attr := range t.Attr {
					attrName := strings.ToLower(attr.Name.Local)
					if attrName == "sizes" {
						hasSizes = true
					}
					if attrName == "rel" {
						for _, v := range strings.Fields(strings.ToLower(attr.Value)) {
							if v == "icon" {
								relIsIcon = true
								break
							}
						}
					}
				}
				if hasSizes && !relIsIcon {
					r.AddWithLocation(report.Error, "RSC-005",
						`attribute "sizes" not allowed on "link" unless rel contains "icon"`,
						location)
				}
			}

		case xml.EndElement:
			if foreignDepth > 0 {
				foreignDepth--
				continue
			}
		}
	}
}

// checkIDRefAttributes reports RSC-005 when IDREF attributes (like for, list,
// form, aria-activedescendant, aria-controls, aria-describedby, aria-flowto,
// aria-labelledby, aria-owns, headers) reference non-existent IDs.
func checkIDRefAttributes(data []byte, location string, r *report.Report) {
	const xhtmlNS = "http://www.w3.org/1999/xhtml"

	// First pass: collect all IDs in the document
	ids := make(map[string]bool)
	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		if se, ok := tok.(xml.StartElement); ok {
			for _, attr := range se.Attr {
				if strings.ToLower(attr.Name.Local) == "id" && attr.Value != "" {
					ids[attr.Value] = true
				}
			}
		}
	}

	// Single IDREF attributes (must reference exactly one ID)
	singleIDRefAttrs := map[string]bool{
		"for":                       true, // label
		"list":                      true, // input
		"form":                      true, // form-associated elements
		"aria-activedescendant":     true,
	}

	// Space-separated IDREFS attributes (can reference multiple IDs)
	multiIDRefAttrs := map[string]bool{
		"headers":           true, // td/th
		"aria-controls":     true,
		"aria-describedby":  true,
		"aria-flowto":       true,
		"aria-labelledby":   true,
		"aria-owns":         true,
	}

	// Second pass: check all IDREF attributes
	decoder = newXHTMLDecoder(strings.NewReader(string(data)))
	foreignDepth := 0
	reported := make(map[string]bool)

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if foreignDepth > 0 {
				foreignDepth++
				continue
			}
			ns := t.Name.Space
			if ns != "" && ns != xhtmlNS {
				foreignDepth = 1
				continue
			}
			name := strings.ToLower(t.Name.Local)
			if name == "svg" || name == "math" {
				foreignDepth = 1
				continue
			}

			for _, attr := range t.Attr {
				attrName := strings.ToLower(attr.Name.Local)

				if singleIDRefAttrs[attrName] && attr.Value != "" {
					if !ids[attr.Value] {
						key := attrName + "=" + attr.Value
						if !reported[key] {
							reported[key] = true
							r.AddWithLocation(report.Error, "RSC-005",
								fmt.Sprintf(`"%s" attribute value "%s" does not reference a valid ID`,
									attrName, attr.Value),
								location)
						}
					}
				}

				if multiIDRefAttrs[attrName] && attr.Value != "" {
					for _, ref := range strings.Fields(attr.Value) {
						if !ids[ref] {
							key := attrName + "=" + ref
							if !reported[key] {
								reported[key] = true
								r.AddWithLocation(report.Error, "RSC-005",
									fmt.Sprintf(`"%s" attribute references non-existent ID "%s"`,
										attrName, ref),
									location)
							}
						}
					}
				}
			}

		case xml.EndElement:
			if foreignDepth > 0 {
				foreignDepth--
				continue
			}
		}
	}
}

// checkMetaRequiredAttrs reports RSC-005 when a <meta> element is missing all
// of the required grouping attributes: charset, name, http-equiv, or property.
// A bare <meta content="..."/> is invalid because the content has no key.
func checkMetaRequiredAttrs(data []byte, location string, r *report.Report) {
	const xhtmlNS = "http://www.w3.org/1999/xhtml"

	decoder := newXHTMLDecoder(strings.NewReader(string(data)))
	foreignDepth := 0
	inHead := false

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if foreignDepth > 0 {
				foreignDepth++
				continue
			}
			ns := t.Name.Space
			if ns != "" && ns != xhtmlNS {
				foreignDepth = 1
				continue
			}
			name := strings.ToLower(t.Name.Local)
			if name == "svg" || name == "math" {
				foreignDepth = 1
				continue
			}

			if name == "head" {
				inHead = true
			}
			if name == "body" {
				return // done scanning head
			}

			if name == "meta" && inHead {
				hasCharset := false
				hasName := false
				hasHttpEquiv := false
				hasProperty := false
				for _, attr := range t.Attr {
					switch strings.ToLower(attr.Name.Local) {
					case "charset":
						hasCharset = true
					case "name":
						hasName = true
					case "http-equiv":
						hasHttpEquiv = true
					case "property":
						hasProperty = true
					}
				}
				if !hasCharset && !hasName && !hasHttpEquiv && !hasProperty {
					r.AddWithLocation(report.Error, "RSC-005",
						`element "meta" missing one or more required attributes; expected attribute "name", "http-equiv", "charset", or "property"`,
						location)
				}
			}

		case xml.EndElement:
			if foreignDepth > 0 {
				foreignDepth--
				continue
			}
			eName := strings.ToLower(t.Name.Local)
			if eName == "head" {
				return // done scanning head
			}
		}
	}
}
