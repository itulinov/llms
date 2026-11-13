export function createAuth(onChange) {
 let generation = 0, controller, user = null;
 async function request(path, body, method = 'GET') {
  controller = new AbortController(); const current = controller;
  const timer = setTimeout(() => current.abort(), 10000);
  try {
   const response = await fetch('/api/' + path, {method, credentials: 'same-origin', cache: 'no-store', signal: current.signal,
    headers: body ? {'Content-Type':'application/json'} : {}, body: body ? JSON.stringify(body) : undefined});
   let data = null; if (response.status !== 204) { try { data = await response.json(); } catch {} }
   if (!response.ok) { const error = new Error('HTTP'); error.status = response.status; error.retry = Number(response.headers.get('Retry-After')) || 300; throw error; }
   return data;
  } finally { clearTimeout(timer); }
 }
 function message(error, action) {
  if (error.status === 401) return action === 'login' ? 'Неверный логин или пароль.' : 'Войдите в систему.';
  if (error.status === 403) return 'Доступ запрещён. Проверьте настройки доступа.';
  if (error.status === 429) return `Слишком много попыток. Повторите через ${error.retry} сек.`;
  return 'Сервис временно недоступен. Повторите запрос.';
 }
 let busy = false;
 async function run(action, body, background = false) {
  const anonymousBackground = background && !user;
  const mine = ++generation; controller?.abort(); busy = true;
  if (action === 'logout') user = null;
  if (!anonymousBackground) onChange({user, busy:true, error:'', action});
  try {
   const data = await request(action === 'check' ? 'auth/me' : 'auth/' + action, body, action === 'check' ? 'GET' : 'POST');
   if (mine !== generation) return;
   user = action === 'logout' ? null : data;
   onChange({user, busy:false, error:'', action});
  } catch (error) {
   if (mine !== generation) return;
   if (anonymousBackground && error.status === 401) return;
   if (error.status === 401) user = null;
   const retry = error.status === 429 ? error.retry : 0;
   onChange({user, busy:false, error:message(error,action), action, retry,
    unavailable: error.status !== 401, logoutPending: action === 'logout'});
  } finally { if (mine === generation) busy = false; }
 }
 function refresh() { if (!busy && document.visibilityState === 'visible') run('check', undefined, true); }
 window.addEventListener('focus',refresh); document.addEventListener('visibilitychange',refresh);
 return {check:()=>run('check'), login:(login,password)=>run('login',{login,password}), logout:()=>run('logout'),
 dispose(){++generation;controller?.abort();window.removeEventListener('focus',refresh);document.removeEventListener('visibilitychange',refresh);}};
}
