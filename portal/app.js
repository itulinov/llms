import { createAuth } from './auth.js';
const el = id => document.getElementById(id);
let state, lockedUntil = 0;
function render(s) {
 state=s;
 if(s.retry) lockedUntil=Date.now()+s.retry*1000;
 el('status').textContent=s.busy?'Проверяем доступ…':s.user?'Вы вошли в систему.':'Вход в корпоративную LMS';
 el('error').textContent=s.error||'';
 el('login').hidden=Boolean(s.busy||s.user||s.unavailable);
 el('profile').hidden=Boolean(s.busy||!s.user||s.error);
 el('fullname').textContent=s.user?.fullname||s.user?.login||'';
 el('username').textContent=s.user?.login||'';
 el('logout').hidden=Boolean(s.busy||(!s.user&&!s.logoutPending));
 el('retry').hidden=Boolean(s.busy||!s.unavailable);
 el('login').querySelector('button').disabled=s.busy||Date.now()<lockedUntil;
}
const client=createAuth(render);
el('login').addEventListener('submit',async e=>{e.preventDefault();const f=e.currentTarget;try{await client.login(f.elements.login.value,f.elements.password.value);}finally{f.elements.password.value='';}});
el('logout').onclick=()=>client.logout();
el('retry').onclick=()=>state.logoutPending?client.logout():client.check();
setInterval(()=>{el('login').querySelector('button').disabled=state?.busy||Date.now()<lockedUntil;},1000);
client.check();
