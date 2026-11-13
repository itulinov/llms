import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
const source=await readFile(new URL('../admin/src/auth.js',import.meta.url),'utf8');
const {createAuth}=await import('data:text/javascript;base64,'+Buffer.from(source).toString('base64'));
globalThis.window=new EventTarget();globalThis.document=new EventTarget();document.visibilityState='visible';
const response=(status,data)=>new Response(status===204?null:JSON.stringify(data),{status});
test('общий код авторизации идентичен',async()=>assert.equal(source,await readFile(new URL('../portal/auth.js',import.meta.url),'utf8')));
test('жизненный цикл и ошибки',async()=>{
 const states=[];const a=createAuth(s=>states.push(s));
 globalThis.fetch=async()=>response(200,{login:'admin',role:'admin'});await a.check();assert.equal(states.at(-1).user.role,'admin');
 globalThis.fetch=async()=>response(503,{});await a.check();assert.equal(states.at(-1).user.login,'admin');assert.equal(states.at(-1).unavailable,true);
 globalThis.fetch=async()=>response(401,{});await a.check();assert.equal(states.at(-1).user,null);
 globalThis.fetch=async()=>response(429,{});await a.login('admin','password');assert.equal(states.at(-1).retry,300);
 globalThis.fetch=async()=>response(204);await a.logout();assert.equal(states.at(-1).user,null);assert.equal(states.at(-1).error,'');a.dispose();
});
test('старый me не восстанавливает кабинет после выхода',async()=>{
 const states=[];const a=createAuth(s=>states.push(s));let finish;
 globalThis.fetch=()=>new Promise(resolve=>finish=resolve);const pending=a.check();
 globalThis.fetch=async()=>response(204);await a.logout();
 finish(response(200,{login:'admin'}));await pending;
 assert.equal(states.at(-1).user,null);a.dispose();
});
test('ошибка выхода скрывает кабинет и сохраняет возможность повторить',async()=>{
 let state;const a=createAuth(s=>state=s);globalThis.fetch=async()=>response(200,{login:'admin'});await a.check();
 globalThis.fetch=async()=>{throw new TypeError('network')};await a.logout();assert.equal(state.user,null);assert.equal(state.logoutPending,true);assert.equal(state.unavailable,true);a.dispose();
});
test('403 не превращается в отсутствие сессии',async()=>{
 let state;const a=createAuth(s=>state=s);globalThis.fetch=async()=>response(200,{role:'employee'});await a.check();globalThis.fetch=async()=>response(403,{});await a.check();assert.equal(state.user.role,'employee');assert.equal(state.unavailable,true);a.dispose();
});
test('фокус не стирает ошибку входа и не скрывает анонимную форму',async()=>{
 const states=[];const a=createAuth(s=>states.push(s));globalThis.fetch=async()=>response(401,{});
 await a.login('ivanov','wrong-password');const before=states.length;
 window.dispatchEvent(new Event('focus'));await new Promise(r=>setTimeout(r,0));
 assert.equal(states.length,before);assert.equal(states.at(-1).error,'Неверный логин или пароль.');
 globalThis.fetch=async()=>response(200,{login:'ivanov'});window.dispatchEvent(new Event('focus'));await new Promise(r=>setTimeout(r,0));assert.equal(states.at(-1).user.login,'ivanov');a.dispose();
});
