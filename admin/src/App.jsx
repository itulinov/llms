import AdminWorkspace from './AdminWorkspace';
import { useEffect, useState, useRef } from 'react';
import { createAuth } from './auth';

export default function App() {
  const [auth, setAuth] = useState({busy:true, user:null});
  const client = useRef(null);
  const [login, setLogin] = useState('');
  const [locked, setLocked] = useState(false);
  const lockedUntil = useRef(0);
  useEffect(() => { const c = createAuth(setAuth); client.current = c; c.check(); return () => c.dispose(); }, []);
  useEffect(() => { if (auth.retry) lockedUntil.current=Date.now()+auth.retry*1000; }, [auth.retry]);
  useEffect(() => {const t=setInterval(()=>setLocked(Date.now()<lockedUntil.current),250);return ()=>clearInterval(t);}, []);
  if (!auth.user || auth.user.role !== 'admin') return <main className="auth-panel">
    <h1>Вход в Admin</h1>
    {auth.busy ? <p role="status">Проверяем доступ…</p> : <>
    {auth.error && <p role="alert" className="error">{auth.error}</p>}
    {auth.user?.role === 'employee' ? <p>Нет доступа к администрированию.</p> : !auth.unavailable && <form onSubmit={async e=>{e.preventDefault();const f=e.currentTarget; const login=f.elements.login.value,password=f.elements.password.value; try {await client.current.login(login,password);} finally {f.elements.password.value='';}}}>
      <label>Логин<input name="login" value={login} onChange={e=>setLogin(e.target.value)} autoComplete="username" required /></label>
      <label>Пароль<input name="password" type="password" autoComplete="current-password" required /></label>
      <button disabled={locked}>Войти</button>
    </form>}
    {(auth.unavailable || auth.user) && <button onClick={()=>auth.logoutPending?client.current.logout():client.current.check()}>Повторить проверку</button>}
    {(auth.user || auth.logoutPending) && <button onClick={()=>client.current.logout()}>Выйти</button>}
    </>}
  </main>;
  return <AdminWorkspace user={auth.user} onLogout={()=>client.current.logout()} onUnauthorized={()=>client.current.check()} authError={auth.error}/>;
}
