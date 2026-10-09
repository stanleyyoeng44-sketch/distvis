// Read-only browser verification of the public API documentation.
import assert from 'node:assert/strict';
const {chromium}=await import(process.env.DISTVIS_PLAYWRIGHT_MODULE || 'playwright');
const base=process.env.DISTVIS_URL || 'http://localhost:3000';
const browser=await chromium.launch({headless:true,...(process.env.DISTVIS_BROWSER_PATH ? {executablePath:process.env.DISTVIS_BROWSER_PATH} : {})});
const context=await browser.newContext({viewport:{width:1440,height:1000},permissions:['clipboard-read','clipboard-write']});
const page=await context.newPage(), errors=[];
page.on('pageerror',e=>errors.push(e.message));
try{
 await page.goto(base);
 await page.locator('#nav-guide').click();
 await page.waitForURL('**/docs/go');
 assert.equal(await page.locator('h1').textContent(),'Go API');
 await page.locator('#docs-search').fill('RPCPeers');
 assert.ok(await page.locator('.doc-section:visible').count()>0);
 assert.ok(await page.locator('.doc-section[hidden]').count()>0);
 await page.locator('#docs-search').fill('not-found-xyz-123');
 assert.ok(await page.locator('#docs-no-results').isVisible());
 await page.locator('#docs-search').fill('');
 await page.locator('.copy-code').first().click();
 assert.equal(await page.evaluate(()=>navigator.clipboard.readText()),'my-protocols/\n  ping/\n    go.mod\n    main.go');
 await page.screenshot({path:'artifacts/api-docs-desktop.png'});
 await page.locator('#docs-toc a').filter({hasText:'Runtime 生命周期'}).click();
 assert.ok((await page.locator('h2').filter({hasText:'Runtime 生命周期'}).boundingBox()).y>=54);
 await page.getByRole('link',{name:'平台 HTTP API 自动化实验与读取事件',exact:true}).click();
 await page.waitForURL('**/docs/platform');
 assert.equal(await page.locator('h1').textContent(),'平台 HTTP API');
 await page.screenshot({path:'artifacts/platform-api-desktop.png'});
 await page.setViewportSize({width:390,height:844});
 await page.reload();
 assert.equal(await page.locator('#docs-toc').getAttribute('open'),null);
 for(const route of ['/docs/platform','/docs/go']){
  await page.goto(base+route);
  assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),route+' overflow');
 }
 await page.screenshot({path:'artifacts/api-docs-mobile.png'});
 await page.getByRole('link',{name:'返回工作台 →'}).click();
 assert.ok(await page.locator('.mobile-docs').isVisible());
 await page.locator('.mobile-docs').click();
 await page.waitForURL('**/docs/go');
 // Existing protocol code and unsaved browser drafts survive a documentation visit.
 const projects=await (await fetch(base+'/api/protocol-projects')).json();
 if(projects.length){
  await page.setViewportSize({width:1440,height:1000});
  const route=`#protocol/${projects[0].id}/code`;
  await page.goto(base+'/'+route);
  await page.locator('#source-editor').waitFor();
  const original=await page.locator('#source-editor').inputValue();
  await page.locator('#source-editor').fill(original+'\n// local documentation navigation check\n');
  await page.locator('#nav-guide').click();
  await page.waitForURL('**/docs/go');
  await page.getByRole('link',{name:'返回工作台 →'}).click();
  await page.locator('#source-editor').waitFor();
  assert.equal(new URL(page.url()).hash,route);
  assert.equal(await page.locator('#source-editor').inputValue(),original+'\n// local documentation navigation check\n');
 }
 assert.deepEqual(errors,[]);
 console.log('PASS: documentation entry, tabs, search, empty state, copy, anchors, mobile overflow, return route and draft preservation; no JS errors');
}finally{await browser.close();}
