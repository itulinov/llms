// Сначала: cd admin && npm install && npx playwright install firefox
import { firefox } from '../admin/node_modules/playwright/index.mjs';
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { resolve, extname } from 'node:path';
import assert from 'node:assert/strict';
const root=resolve(import.meta.dirname,'..');
const server=createServer(async(req,res)=>{
 const url=new URL(req.url,'http://localhost');
 const path=url.pathname.startsWith('/portal/')?url.pathname.slice(1):'admin/dist'+url.pathname;
 try {const file=resolve(root,path.endsWith('/')?path+'index.html':path);if(!file.startsWith(root+'/'))throw Error();const data=await readFile(file);res.setHeader('Content-Type',({'.js':'application/javascript','.css':'text/css','.html':'text/html'})[extname(file)]||'application/octet-stream');res.end(data);}catch{res.statusCode=404;res.end();}
});
await new Promise(r=>server.listen(0,'127.0.0.1',r));
let browser;
try {
 browser=await firefox.launch();
 // Реальный fetch в браузере при зависшем HTTP-запросе должен завершиться по таймауту.
 {
 const context=await browser.newContext();const page=await context.newPage();
 let calls=0;const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.route('**/api/**',route=>{calls++; /* Не отвечаем: имитируем зависший API. */});
 await page.goto(`http://127.0.0.1:${server.address().port}/portal/`);
 await page.getByText('Сервис временно недоступен. Повторите запрос.',{exact:true}).waitFor({timeout:13000});
 assert.equal(await page.locator('#status').textContent(),'Вход в корпоративную LMS');
 assert.equal(calls,1);assert.deepEqual(errors,[]);
 await context.close();console.log('PASS Firefox: зависший API прекращает проверку через 10 секунд');
 }
 for (const path of ['/','/portal/']) {
 const context=await browser.newContext();const page=await context.newPage();page.setDefaultTimeout(10000);let user=null;let mode=200;
 const admin={id:'a',login:'admin',role:'admin',fullname:null,collaborator_id:null};
 await page.route('**/api/**',async route=>{
  const url=new URL(route.request().url());
  let status=200,data;
  if(url.pathname.endsWith('/login')){if(mode!==200){status=mode;data={error:'error'};}else{user={...admin};data=user;}}
  else if(url.pathname.endsWith('/logout')){user=null;status=204;}
  else if(url.pathname.endsWith('/me')){status=user?200:401;data=user||{error:'Unauthorized'};}
  else if(url.pathname==='/api/admin/collaborators')data={items:[],total:0};
  else data={status:'ok',database:'connected'};
  await route.fulfill({status,headers:{'Content-Type':'application/json','Retry-After':'1'},body:status===204?'':JSON.stringify(data)});
 });
 const base=`http://127.0.0.1:${server.address().port}`;
 await page.goto(base+path);
 await page.getByRole('button',{name:'Войти',exact:true}).waitFor({state:'visible'});
 await page.getByLabel('Логин',{exact:true}).fill('admin');await page.getByLabel('Пароль',{exact:true}).fill('valid-password-123');
 mode=401;await page.getByRole('button',{name:'Войти',exact:true}).click();await page.getByText('Неверный логин или пароль.',{exact:true}).waitFor();
 assert.equal(await page.getByLabel('Пароль',{exact:true}).inputValue(),'');
 mode=200;await page.getByLabel('Пароль',{exact:true}).fill('valid-password-123');await page.getByRole('button',{name:'Войти',exact:true}).click();
 await page.getByRole('button',{name:'Выйти',exact:true}).waitFor();
 await page.reload();await page.getByRole('button',{name:'Выйти',exact:true}).waitFor();
 user={...admin,role:'employee',fullname:'Иванов Иван'};await page.reload();
 if(path==='/')await page.getByText('Нет доступа к администрированию.',{exact:true}).waitFor();
 else await page.getByRole('heading',{name:'Иванов Иван'}).waitFor();
 await page.getByRole('button',{name:'Выйти',exact:true}).click();await page.getByRole('button',{name:'Войти',exact:true}).waitFor({state:'visible'});
 mode=503;await page.getByLabel('Логин',{exact:true}).fill('admin');await page.getByLabel('Пароль',{exact:true}).fill('valid-password-123');await page.getByRole('button',{name:'Войти',exact:true}).click();await page.getByText('Сервис временно недоступен. Повторите запрос.',{exact:true}).waitFor();
 await page.setViewportSize({width:375,height:812});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
 await context.close();console.log(`PASS Firefox ${path}`);
 }
}finally{await browser?.close();await new Promise(r=>server.close(r));}
