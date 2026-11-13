// Browser regression checks using Chromium's DevTools pipe and the Go standard library.
// Start the preview, then run: go run scripts/verify-site.go
package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type message struct {
	ID     int             `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}
type browser struct {
	in      *os.File
	out     chan message
	seq     int
	session string
	events  []message
}

func (b *browser) call(method string, params any) (json.RawMessage, error) {
	b.seq++
	m := map[string]any{"id": b.seq, "method": method, "params": params}
	if b.session != "" {
		m["sessionId"] = b.session
	}
	data, _ := json.Marshal(m)
	if _, err := b.in.Write(append(data, 0)); err != nil {
		return nil, err
	}
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	for {
		select {
		case r, ok := <-b.out:
			if !ok {
				return nil, fmt.Errorf("Chromium closed its pipe")
			}
			if r.ID == b.seq {
				if len(r.Error) > 0 {
					return nil, fmt.Errorf("%s: %s", method, r.Error)
				}
				return r.Result, nil
			}
			if r.Method != "" {
				b.events = append(b.events, r)
			}
		case <-timer.C:
			return nil, fmt.Errorf("timeout: %s", method)
		}
	}
}
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
func (b *browser) eval(expression string) any {
	r := must(b.call("Runtime.evaluate", map[string]any{"expression": expression, "awaitPromise": true, "returnByValue": true, "userGesture": true}))
	var x struct {
		Result struct {
			Value any `json:"value"`
		}
		ExceptionDetails json.RawMessage `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(r, &x); err != nil {
		panic(err)
	}
	if len(x.ExceptionDetails) > 0 {
		panic(string(x.ExceptionDetails))
	}
	return x.Result.Value
}
func (b *browser) wait(expression string) {
	for i := 0; i < 100; i++ {
		r, err := b.call("Runtime.evaluate", map[string]any{"expression": expression, "returnByValue": true})
		if err == nil && strings.Contains(string(r), `"value":true`) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	panic("wait failed: " + expression)
}
func (b *browser) navigate(path string) {
	must(b.call("Page.navigate", map[string]any{"url": "http://127.0.0.1:4173" + path}))
	b.wait(`document.readyState === 'complete' && !!document.querySelector('#main-content')`)
	time.Sleep(100 * time.Millisecond)
}
func (b *browser) screenshot(name string) {
	r := must(b.call("Page.captureScreenshot", map[string]any{"format": "png", "captureBeyondViewport": false}))
	var x struct{ Data string }
	must(0, json.Unmarshal(r, &x))
	must(0, os.WriteFile("tmp/site-test/"+name+".png", must(base64.StdEncoding.DecodeString(x.Data)), 0644))
}
func main() {
	must(0, os.MkdirAll("tmp/site-test", 0755))
	profile := must(os.MkdirTemp("tmp/site-test", "chromium-profile-"))
	defer os.RemoveAll(profile)
	readIn, writeIn := must2(os.Pipe())
	readOut, writeOut := must2(os.Pipe())
	cmd := exec.Command("chromium", "--headless", "--disable-gpu", "--no-first-run", "--no-default-browser-check", "--disable-background-networking", "--disable-component-update", "--disable-features=Translate", "--remote-debugging-pipe", "--user-data-dir="+profile, "about:blank")
	log := must(os.Create("tmp/site-test/chromium.log"))
	defer log.Close()
	cmd.ExtraFiles = []*os.File{readIn, writeOut}
	cmd.Stderr = log
	cmd.Stdout = log
	must(0, cmd.Start())
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	readIn.Close()
	writeOut.Close()
	defer writeIn.Close()
	defer readOut.Close()
	b := &browser{in: writeIn, out: make(chan message, 2000)}
	go func() {
		defer close(b.out)
		r := bufio.NewReader(readOut)
		for {
			line, e := r.ReadBytes(0)
			if e != nil {
				return
			}
			var m message
			if json.Unmarshal(line[:len(line)-1], &m) == nil {
				b.out <- m
			}
		}
	}()
	r := must(b.call("Target.createTarget", map[string]any{"url": "about:blank"}))
	var target struct{ TargetID string }
	must(0, json.Unmarshal(r, &target))
	r = must(b.call("Target.attachToTarget", map[string]any{"targetId": target.TargetID, "flatten": true}))
	var session struct{ SessionID string }
	must(0, json.Unmarshal(r, &session))
	b.session = session.SessionID
	must(b.call("Page.enable", map[string]any{}))
	must(b.call("Runtime.enable", map[string]any{}))
	must(b.call("Network.enable", map[string]any{}))
	results := []map[string]any{}
	check := func(name, expression string) {
		v := b.eval(expression)
		pass := v == true
		results = append(results, map[string]any{"name": name, "pass": pass, "result": v})
		fmt.Printf("%s: %v\n", name, v)
	}
	metrics := func(w, h int) {
		must(b.call("Emulation.setDeviceMetricsOverride", map[string]any{"width": w, "height": h, "deviceScaleFactor": 1, "mobile": w <= 520, "screenWidth": w, "screenHeight": h}))
	}
	metrics(1440, 1000)
	b.navigate("/index.html")
	check("home semantics and exact 73 chapter links", `document.querySelectorAll('main').length===1 && document.documentElement.lang==='en' && document.querySelectorAll('h1').length===1 && document.querySelectorAll('.chapter-list a').length===73 && document.querySelector('.skip-link').getAttribute('href')==='#main-content' && !!document.querySelector('label[for="chapter-filter"]')`)
	check("neutral light palette and reading width", `document.documentElement.dataset.theme==='light' && getComputedStyle(document.body).backgroundColor==='rgb(252, 252, 252)' && document.querySelector('main').getBoundingClientRect().width<=752 && document.documentElement.scrollWidth<=innerWidth`)
	contrast := `(()=>{const s=getComputedStyle(document.documentElement),rgb=x=>x.match(/[\da-f]{2}/gi).map(n=>parseInt(n,16)/255).map(n=>n<=.04045?n/12.92:((n+.055)/1.055)**2.4),lum=x=>{const v=rgb(x);return v[0]*.2126+v[1]*.7152+v[2]*.0722},ratio=(a,b)=>{const x=lum(a),y=lum(b);return (Math.max(x,y)+.05)/(Math.min(x,y)+.05)},v=n=>s.getPropertyValue(n).trim();return ['--text','--subtext0','--subtext1','--link','--accent'].every(n=>ratio(v(n),v('--base'))>=4.5&&ratio(v(n),v('--mantle'))>=4.5)})()`
	check("light text colors meet 4.5 contrast", contrast)
	b.screenshot("desktop-light")
	check("both ebook downloads are named and bypass HTMX", `(()=>{const expected={epub:'snegov-dictator.epub',kepub:'snegov-dictator-kepub.kepub'};return Object.entries(expected).every(([format,name])=>{const a=document.querySelector('.ebook-downloads a[data-format="'+format+'"]');return !!a&&a.getAttribute('href')===name&&a.download===name&&a.getAttribute('hx-boost')==='false'})})()`)
	downloadDir := must(os.MkdirTemp("tmp/site-test", "kobo-download-"))
	defer os.RemoveAll(downloadDir)
	absoluteDownloadDir := must(filepath.Abs(downloadDir))
	activeSession := b.session
	b.session = ""
	must(b.call("Browser.setDownloadBehavior", map[string]any{"behavior": "allow", "downloadPath": absoluteDownloadDir}))
	b.session = activeSession
	b.eval(`window.__koboDownloadMarker=2468;window.__koboInitialUrl=location.href;window.__koboRequests=0;document.addEventListener('htmx:beforeRequest',()=>window.__koboRequests++)`)
	for _, book := range []struct{ format, name string }{{"epub", "snegov-dictator.epub"}, {"kepub", "snegov-dictator-kepub.kepub"}} {
		wantDownload := must(os.ReadFile("web/" + book.name))
		b.eval(fmt.Sprintf(`document.querySelector('.ebook-downloads a[data-format="%s"]').click()`, book.format))
		downloadMatches := false
		for i := 0; i < 100; i++ {
			data, err := os.ReadFile(filepath.Join(downloadDir, book.name))
			if err == nil && bytes.Equal(data, wantDownload) {
				downloadMatches = true
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		results = append(results, map[string]any{"name": book.format + " browser download matches the validated artifact", "pass": downloadMatches, "bytes": len(wantDownload)})
	}
	check("Kobo download preserves the reading page", `window.__koboDownloadMarker===2468 && location.href===window.__koboInitialUrl && window.__koboRequests===0`)
	b.eval(`window.__verifyMarker=12345;window.__verifyTimeOrigin=performance.timeOrigin;window.__hxRequests=[];document.addEventListener('htmx:beforeRequest',e=>window.__hxRequests.push(e.detail.requestConfig.path));document.querySelector('a[data-page-id="p1-c01"]').click()`)
	b.wait(`location.pathname==='/chapters/p1-c01.html' && document.querySelector('main').dataset.pageId==='p1-c01'`)
	check("HTMX chapter navigation preserves document", `window.__verifyMarker===12345 && performance.timeOrigin===window.__verifyTimeOrigin && window.__hxRequests.length===1 && document.title.includes('The Thursday Warning') && document.querySelector('nav [aria-current="page"]').dataset.pageId==='p1-c01'`)
	check("chapter content and source bypass", `document.querySelector('article').innerText.length>500 && [...document.querySelectorAll('article a[href]')].filter(a=>/\/(source|evidence)\//.test(a.href)).length>=1 && [...document.querySelectorAll('article a[href]')].filter(a=>/\/(source|evidence)\//.test(a.href)).every(a=>a.getAttribute('hx-boost')==='false'||a.getAttribute('data-hx-boost')==='false')`)
	b.screenshot("desktop-chapter-light")
	b.eval(`document.querySelector('.page-navigation .next').click()`)
	b.wait(`document.querySelector('main').dataset.pageId==='p1-c02'`)
	check("next chapter", `location.pathname==='/chapters/p1-c02.html' && window.__verifyMarker===12345`)
	b.eval(`document.querySelector('.page-navigation .previous').click()`)
	b.wait(`document.querySelector('main').dataset.pageId==='p1-c01'`)
	check("previous chapter", `location.pathname==='/chapters/p1-c01.html' && window.__verifyMarker===12345`)
	b.eval(`history.back()`)
	b.wait(`document.querySelector('main').dataset.pageId==='p1-c02'`)
	check("history back", `location.pathname==='/chapters/p1-c02.html' && document.querySelector('nav [aria-current="page"]').dataset.pageId==='p1-c02'`)
	b.eval(`history.forward()`)
	b.wait(`document.querySelector('main').dataset.pageId==='p1-c01'`)
	check("history forward", `location.pathname==='/chapters/p1-c01.html' && window.__verifyMarker===12345`)
	b.eval(`document.getElementById('theme-toggle').click()`)
	check("Mocha dark palette", `document.documentElement.dataset.theme==='dark' && getComputedStyle(document.body).backgroundColor==='rgb(30, 30, 46)' && getComputedStyle(document.body).color==='rgb(205, 214, 244)' && localStorage.getItem('dictator-theme')==='dark' && document.getElementById('theme-toggle').getAttribute('aria-label')==='Switch to light mode'`)
	check("dark text colors meet 4.5 contrast", contrast)
	b.eval(`window.scrollTo(0,0)`)
	b.screenshot("desktop-dark")
	b.eval(`document.querySelector('a[data-page-id="catalog-index"]').click()`)
	b.wait(`document.querySelector('main').dataset.pageId==='catalog-index'`)
	b.eval(`document.querySelector('article a[href*="parts/part-1-ledger"]').click()`)
	b.wait(`location.pathname==='/catalog/parts/part-1-ledger.html'`)
	check("nested catalog and persistent shell links", `document.documentElement.dataset.theme==='dark' && window.__verifyMarker===12345 && document.querySelector('a[data-page-id="p1-c01"]').pathname==='/chapters/p1-c01.html' && document.querySelector('.book-brand').pathname==='/index.html' && document.querySelector('a[data-page-id="catalog-characters"]').pathname==='/catalog/characters.html'`)
	b.eval(`document.querySelector('a[data-page-id="p1-c01"]').click()`)
	b.wait(`document.querySelector('main').dataset.pageId==='p1-c01'`)
	b.eval(`document.querySelector('article a[href*="source-map.html#"]').click()`)
	b.wait(`location.pathname==='/catalog/source-map.html' && location.hash==='#p1-c01'`)
	time.Sleep(300 * time.Millisecond)
	check("boosted hash target position", `(()=>{const r=document.getElementById('p1-c01').getBoundingClientRect();return r.top>=0&&r.top<100&&document.activeElement.id==='main-content'})()`)
	must(b.call("Page.reload", map[string]any{}))
	b.wait(`document.readyState==='complete' && !!document.querySelector('main')`)
	time.Sleep(100 * time.Millisecond)
	check("theme persists on refresh", `document.documentElement.dataset.theme==='dark' && localStorage.getItem('dictator-theme')==='dark'`)
	b.navigate("/catalog/parts/part-6-ledger.html")
	check("theme and links on direct nested load", `document.documentElement.dataset.theme==='dark' && document.querySelector('a[data-page-id="p6-c15"]').pathname==='/chapters/p6-c15.html' && document.querySelector('a[data-page-id="overview"]').pathname==='/index.html'`)
	b.eval(`const f=document.getElementById('chapter-filter');f.value='Thursday';f.dispatchEvent(new Event('input',{bubbles:true}))`)
	check("chapter filter one result", `document.getElementById('filter-status').innerText==='1 chapter found.' && [...document.querySelectorAll('.chapter-list li')].filter(x=>!x.hidden).length===1`)
	b.eval(`document.getElementById('chapter-filter').value='zzzzzznone';document.getElementById('chapter-filter').dispatchEvent(new Event('input',{bubbles:true}))`)
	check("chapter filter zero results", `document.getElementById('filter-status').innerText==='0 chapters found.' && [...document.querySelectorAll('.contents-group')].every(x=>x.hidden)`)
	b.eval(`document.getElementById('chapter-filter').dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true}))`)
	check("escape clears filter", `document.getElementById('chapter-filter').value==='' && document.getElementById('filter-status').hidden && [...document.querySelectorAll('.chapter-list li')].every(x=>!x.hidden)`)
	b.navigate("/index.html")
	must(b.call("Input.dispatchKeyEvent", map[string]any{"type": "keyDown", "key": "Tab", "code": "Tab", "windowsVirtualKeyCode": 9}))
	must(b.call("Input.dispatchKeyEvent", map[string]any{"type": "keyUp", "key": "Tab", "code": "Tab", "windowsVirtualKeyCode": 9}))
	check("keyboard skip link and visible focus", `document.activeElement.classList.contains('skip-link') && document.activeElement.matches(':focus-visible') && getComputedStyle(document.activeElement).outlineStyle==='solid' && document.activeElement.getBoundingClientRect().top>=0`)
	must(b.call("Input.dispatchKeyEvent", map[string]any{"type": "keyDown", "key": "Enter", "code": "Enter", "windowsVirtualKeyCode": 13}))
	must(b.call("Input.dispatchKeyEvent", map[string]any{"type": "keyUp", "key": "Enter", "code": "Enter", "windowsVirtualKeyCode": 13}))
	check("keyboard skip reaches main", `document.activeElement.id==='main-content'`)
	b.navigate("/index.html")
	for i := 0; i < 3; i++ {
		must(b.call("Input.dispatchKeyEvent", map[string]any{"type": "keyDown", "key": "Tab", "code": "Tab", "windowsVirtualKeyCode": 9}))
		must(b.call("Input.dispatchKeyEvent", map[string]any{"type": "keyUp", "key": "Tab", "code": "Tab", "windowsVirtualKeyCode": 9}))
	}
	check("theme button keyboard label and focus", `document.activeElement.id==='theme-toggle' && document.activeElement.getAttribute('aria-label')==='Switch to light mode' && document.activeElement.matches(':focus-visible') && getComputedStyle(document.activeElement).outlineStyle==='solid'`)
	must(b.call("Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-reduced-motion", "value": "reduce"}}}))
	check("reduced motion", `matchMedia('(prefers-reduced-motion: reduce)').matches && getComputedStyle(document.getElementById('navigation-progress')).transitionDuration==='0s'`)
	metrics(390, 844)
	b.navigate("/chapters/p4-c15.html")
	check("mobile dark layout", `document.documentElement.dataset.theme==='dark' && !document.getElementById('site-contents').open && document.documentElement.scrollWidth<=innerWidth && document.querySelector('main').getBoundingClientRect().width<360`)
	b.screenshot("mobile-dark")
	b.eval(`document.getElementById('theme-toggle').click()`)
	check("mobile light palette", `document.documentElement.dataset.theme==='light' && getComputedStyle(document.body).backgroundColor==='rgb(252, 252, 252)'`)
	b.screenshot("mobile-light")
	b.eval(`document.querySelector('#site-contents>summary').click();document.getElementById('chapter-filter').value='Thursday';document.getElementById('chapter-filter').dispatchEvent(new Event('input',{bubbles:true}))`)
	check("mobile contents filter", `document.getElementById('site-contents').open && document.getElementById('filter-status').innerText==='1 chapter found.' && document.documentElement.scrollWidth<=innerWidth`)
	b.screenshot("mobile-contents")
	b.eval(`document.querySelector('a[data-page-id="p1-c01"]').click()`)
	b.wait(`document.querySelector('main').dataset.pageId==='p1-c01'`)
	check("mobile navigation closes contents", `!document.getElementById('site-contents').open && document.documentElement.scrollWidth<=innerWidth && document.activeElement.id==='main-content'`)
	metrics(320, 640)
	b.navigate("/catalog/source-map.html")
	check("320px catalog tables have no page overflow", `document.documentElement.scrollWidth<=innerWidth && document.querySelectorAll('table').length>0 && [...document.querySelectorAll('table')].every(t=>t.getBoundingClientRect().right<=innerWidth && getComputedStyle(t).overflowX==='auto')`)
	b.screenshot("mobile-320-catalog")
	b.eval(`document.querySelector('table').scrollIntoView()`)
	b.screenshot("mobile-320-table")
	b.navigate("/index.html")
	check("both downloads fit the 320px overview", `document.documentElement.scrollWidth<=innerWidth && [...document.querySelectorAll('.download-button')].every(a=>a.getBoundingClientRect().right<=innerWidth&&a.getBoundingClientRect().height>=40)`)
	b.eval(`document.querySelector('.ebook-downloads').scrollIntoView({block:'center'})`)
	b.screenshot("mobile-320-kobo-download")
	// With JavaScript disabled, direct pages and a normal anchor still work.
	must(b.call("Emulation.setScriptExecutionDisabled", map[string]any{"value": true}))
	b.navigate("/index.html")
	check("no-script ebook download links", `document.querySelectorAll('.ebook-downloads a[download]').length===2 && !window.htmx`)
	b.navigate("/chapters/p1-c01.html")
	check("no-script direct chapter", `document.querySelector('h1').textContent==='The Thursday Warning' && document.querySelector('article').innerText.length>500 && document.getElementById('theme-toggle').hidden && document.querySelector('.chapter-search').hidden`)
	b.eval(`document.querySelector('.page-navigation .next').click()`)
	b.wait(`location.pathname==='/chapters/p1-c02.html' && document.querySelector('h1').textContent==='The War Turns'`)
	check("no-script next uses full navigation", `document.querySelector('main').dataset.pageId==='p1-c02' && !window.htmx`)
	b.navigate("/catalog/parts/part-1-ledger.html")
	check("no-script direct nested catalog", `document.querySelector('main').dataset.pageId==='catalog-parts-part-1-ledger' && document.querySelector('article').innerText.length>500 && !window.htmx`)
	must(b.call("Emulation.setScriptExecutionDisabled", map[string]any{"value": false}))
	b.navigate("/chapters/p1-c01.html")
	b.eval(`window.__rawMarker=987;document.querySelector('article a[href*="/source/"]').click()`)
	b.wait(`location.pathname==='/source/text/part0002.html' && document.readyState==='complete'`)
	check("source text click uses full document", `!window.__rawMarker && !document.getElementById('main-content') && /[А-Яа-я]/.test(document.body.innerText)`)
	b.navigate("/catalog/source-map.html")
	b.eval(`window.__rawMarker=987;document.querySelector('article a[href*="/evidence/"][href$=".txt"]').click()`)
	b.wait(`location.pathname.endsWith('.txt') && document.readyState==='complete'`)
	check("cached chapter text click uses full document", `!window.__rawMarker && !document.getElementById('main-content') && /[А-Яа-я]/.test(document.body.innerText)`)
	// Check all expected chapter IDs independently of the generator's checks.
	expected := []int{10, 9, 13, 15, 11, 15}
	coverage := true
	for p, count := range expected {
		for c := 1; c <= count; c++ {
			path := fmt.Sprintf("site/chapters/p%d-c%02d.html", p+1, c)
			data, e := os.ReadFile(path)
			if e != nil || !strings.Contains(string(data), fmt.Sprintf(`data-page-id="p%d-c%02d"`, p+1, c)) {
				coverage = false
			}
		}
	}
	chapterFiles := must(filepath.Glob("site/chapters/*.html"))
	results = append(results, map[string]any{"name": "all expected 73 chapter files", "pass": coverage && len(chapterFiles) == 73, "count": len(chapterFiles)})
	for _, path := range []string{"/assets/favicon.svg", "/source/text/part0002.html", "/evidence/chapter-text/P1-C01.txt", "/catalog/parts/part-6-ledger.html"} {
		resp, e := http.Get("http://127.0.0.1:4173" + path)
		pass := e == nil
		detail := ""
		if e == nil {
			data, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			pass = resp.StatusCode == 200 && len(data) > 0
			detail = resp.Header.Get("Content-Type")
			if strings.HasSuffix(path, "favicon.svg") {
				pass = pass && strings.Contains(string(data), "<svg") && strings.Contains(detail, "image/svg+xml")
			}
		}
		results = append(results, map[string]any{"name": "HTTP " + path, "pass": pass, "contentType": detail})
	}
	runtimeErrors := []json.RawMessage{}
	failedAssets := []string{}
	hxRequests := 0
	for _, event := range b.events {
		if event.Method == "Runtime.exceptionThrown" {
			runtimeErrors = append(runtimeErrors, event.Params)
		}
		if event.Method == "Network.requestWillBeSent" && strings.Contains(string(event.Params), `"HX-Request":"true"`) {
			hxRequests++
		}
		if event.Method == "Network.responseReceived" {
			var x struct {
				Response struct {
					URL    string
					Status int
				}
			}
			if json.Unmarshal(event.Params, &x) == nil && x.Response.Status >= 400 && strings.Contains(x.Response.URL, "/assets/") {
				failedAssets = append(failedAssets, x.Response.URL)
			}
		}
	}
	results = append(results, map[string]any{"name": "no browser runtime exceptions", "pass": len(runtimeErrors) == 0, "errors": runtimeErrors})
	results = append(results, map[string]any{"name": "network observed real HTMX requests", "pass": hxRequests > 0, "count": hxRequests})
	results = append(results, map[string]any{"name": "site assets return no HTTP errors after navigation", "pass": len(failedAssets) == 0, "urls": failedAssets})
	must(0, os.WriteFile("tmp/site-test/results.json", must(json.MarshalIndent(results, "", "  ")), 0644))
	must(0, os.WriteFile("tmp/site-test/cdp-events.json", must(json.MarshalIndent(b.events, "", "  ")), 0644))
	failures := 0
	for _, r := range results {
		if r["pass"] != true {
			failures++
			fmt.Printf("FAIL: %s\n", r["name"])
		}
	}
	fmt.Printf("Completed %d checks; %d failures. Screenshots: tmp/site-test/\n", len(results), failures)
	if failures > 0 {
		panic("browser verification failed")
	}
}
func must2(a, b *os.File, err error) (*os.File, *os.File) {
	if err != nil {
		panic(err)
	}
	return a, b
}
