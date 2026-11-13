import {firefox} from '../admin/node_modules/playwright/index.mjs';
import {createServer} from 'node:http';import {readFile} from 'node:fs/promises';import {resolve,extname} from 'node:path';import assert from 'node:assert/strict';
const root=resolve(import.meta.dirname,'../admin/dist');
const server=createServer(async(req,res)=>{try{const path=resolve(root,'.'+(req.url==='/'?'/index.html':req.url));if(!path.startsWith(root+'/'))throw Error();res.setHeader('Content-Type',extname(path)==='.js'?'application/javascript':extname(path)==='.css'?'text/css':'text/html');res.end(await readFile(path))}catch{res.statusCode=404;res.end()}});await new Promise(r=>server.listen(0,'127.0.0.1',r));let browser;
try{browser=await firefox.launch();const page=await browser.newPage();page.setDefaultTimeout(10000);page.on('pageerror',e=>console.error(e.message));
const org={id:'org',name:'Организация',updated_at:'2026-11-13T00:00:00.000001Z'};
const sub={id:'sub',name:'Разработка',org_id:'org',parent_id:null,status:'active',updated_at:org.updated_at};
let position={id:'pos',name:'Разработчик',subdivision_id:'sub',status:'active',collaborator_id:null,updated_at:org.updated_at};
await page.route('**/api/**',async route=>{const r=route.request(),url=new URL(r.url()),path=url.pathname;let data;
if(path==='/api/auth/me')data={login:'admin',role:'admin'};
else if(path==='/api/admin/health')data={status:'ok',database:'connected'};
else if(path==='/api/admin/orgs')data={items:[org],total:1};
else if(path==='/api/admin/subdivisions')data={items:[sub],total:1};
else if(path==='/api/admin/positions')data={items:[position],total:1};
else if(path==='/api/admin/collaborators')data={items:[{id:'employee',fullname:'Иванов Иван'}],total:1};
else if(path.endsWith('/assign')){position={...position,collaborator_id:'employee'};data={position,collaborator:{main_position_id:'pos'},positions:[position]};}
else if(path.endsWith('/unassign')){position={...position,collaborator_id:null};data={position,collaborator:{main_position_id:null},positions:[]};}
else data=position;
await route.fulfill({status:200,json:data});});
await page.goto(`http://127.0.0.1:${server.address().port}/`);
await page.getByRole('button',{name:'Оргструктура',exact:true}).click();
await page.getByRole('button',{name:'Должности',exact:true}).click();
await page.getByRole('button',{name:'Разработчик',exact:true}).click();
await page.getByRole('button',{name:'Найти сотрудника',exact:true}).click();
await page.getByLabel('Сотрудник',{exact:true}).selectOption('employee');
await page.getByRole('button',{name:'Закрепить',exact:true}).click();
await page.getByRole('button',{name:'Открепить',exact:true}).waitFor();
await page.getByRole('button',{name:'Открепить',exact:true}).click();
await page.getByRole('button',{name:'Закрепить',exact:true}).waitFor();
await page.setViewportSize({width:375,height:812});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
await page.getByRole('button',{name:'Закрыть карточку',exact:true}).click();
await page.getByRole('button',{name:'Подразделения',exact:true}).click();
await page.getByRole('button',{name:'Разработка',exact:true}).waitFor();
console.log('PASS Firefox: таблицы оргструктуры, назначения, мобильная навигация');
}finally{await browser?.close();await new Promise(r=>server.close(r))}
