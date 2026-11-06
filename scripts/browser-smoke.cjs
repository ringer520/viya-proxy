async (page) => {
  const base = 'http://127.0.0.1:18765'
  await page.request.post(base+'/api/v1/auth/setup',{headers:{Origin:base},data:{username:'admin',password:'ViyaSmoke2026!'}})
  const login=await page.request.post(base+'/api/v1/auth/login',{headers:{Origin:base},data:{username:'admin',password:'ViyaSmoke2026!'}})
  if(!login.ok())throw Error('Smoke login failed: '+login.status())
  await page.goto(base)
  await page.evaluate(()=>localStorage.setItem('viya-language','zh-CN'))
  await page.reload()
  await page.getByRole('button',{name:'面板设置',exact:true}).waitFor()
  await page.getByRole('button',{name:'面板设置',exact:true}).click()
  await page.getByRole('tab',{name:'存储',exact:true}).click()
  await page.getByText('数据目录',{exact:false}).first().waitFor()
  const storage=await page.request.get(base+'/api/v1/storage')
  if(!storage.ok()||(await storage.json()).schema_version<4)throw Error('Storage/migration endpoint failed')
  await page.getByRole('button',{name:'关闭面板设置',exact:true}).click()
  for(const id of ['proxy-sites','layer4','ddns','certificates','dns-providers','ip-bans','access-logs','config-history']){
    await page.locator('aside a[href="#'+id+'"]').click()
    await page.locator('main').waitFor()
  }
  // A page loads the lists its own dialogs pick from: the DDNS form needs the
  // DNS credential list even though the page itself only renders records.
  await page.goto(base+'/#ddns')
  const seen=[]
  page.on('request',r=>{ if(r.url().includes('/api/v1/'))seen.push(r.url()) })
  await page.reload()
  await page.getByText('动态 DNS',{exact:false}).first().waitFor()
  await page.waitForTimeout(1500)
  if(!seen.some(url=>url.includes('/api/v1/dns-providers')))
    throw Error('DDNS page did not load the DNS credential list its dialog needs')
  if(seen.some(url=>url.includes('/api/v1/ip-bans')))
    throw Error('DDNS page loaded data no part of it renders')
  // A local list failure must not turn a healthy engine into "offline".
  // The overview is the page that loads every data group, so the injected
  // failure is actually requested there — a narrower page would never ask for
  // dns-providers and the banner would never appear.
  await page.locator('aside a[href="#overview"]').click()
  await page.locator('main').waitFor()
  await page.route('**/api/v1/dns-providers',route=>route.fulfill({status:503,json:{error:'Smoke failure'}}))
  await page.reload()
  await page.getByText('引擎正常',{exact:true}).waitFor()
  await page.getByText(/部分数据刷新失败/).first().waitFor()
  await page.unroute('**/api/v1/dns-providers')
  await page.reload()
  await page.getByText('引擎正常',{exact:true}).waitFor()
  await page.screenshot({path:'output/playwright/smoke.png',fullPage:true})
  console.log('PASS: login, navigation, storage schema, partial-refresh isolation')
}
