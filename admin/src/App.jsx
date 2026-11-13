import { useEffect, useState, useRef } from 'react';
import Employees from './Employees';
import Structure from './Structure';
import { createAuth } from './auth';

export default function App() {
  const [auth, setAuth] = useState({busy:true, user:null});
  const client = useRef(null);
  const [section,setSection]=useState('dashboard');
  const [login, setLogin] = useState('');
  const [locked, setLocked] = useState(false);
  const lockedUntil = useRef(0);
  useEffect(() => { const c = createAuth(setAuth); client.current = c; c.check(); return () => c.dispose(); }, []);
  useEffect(() => { if (auth.retry) lockedUntil.current=Date.now()+auth.retry*1000; }, [auth.retry]);
  useEffect(() => {const t=setInterval(()=>setLocked(Date.now()<lockedUntil.current),250);return ()=>clearInterval(t);}, []);
  const [health, setHealth] = useState({ loading: true });

  async function checkApi() {
    setHealth({ loading: true });
    try {
      const response = await fetch('/api/admin/health', { cache: 'no-store' });
      const data = await response.json();
      if (response.status === 401 || response.status === 403) { client.current.check(); return; }
      setHealth({ ...data, httpOk: response.ok, loading: false });
    } catch (error) {
      setHealth({ loading: false, httpOk: false, error: error.message });
    }
  }

  useEffect(() => { if(auth.user?.role === "admin") checkApi(); }, [auth.user]);

  const ready = health.httpOk && health.status === 'ok' && health.database === 'connected';

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
  return (
    <div className="layout">
      <aside className="sidebar">
        <div className="brand">Corporate LMS <small>Администрирование</small></div>
        <nav aria-label="Главное меню">
          <button onClick={()=>{if(section==='dashboard'||window.confirm('Перейти на главную? Несохранённые изменения будут потеряны.'))setSection('dashboard')}}>Главная</button>
          <button onClick={()=>{if(section!=='structure'||window.confirm('Покинуть оргструктуру? Несохранённые изменения будут потеряны.'))setSection('employees')}}>Сотрудники</button><button onClick={()=>{if(section==='dashboard'||window.confirm('Покинуть карточку? Несохранённые изменения будут потеряны.'))setSection('structure')}}>Оргструктура</button>
          <span>Курсы — скоро</span>
          <span>Назначения — скоро</span>
          <span>Настройки — скоро</span>
        </nav>
      </aside>
      <main id="dashboard" className="content">
        {auth.error&&<p role="alert" className="error">{auth.error} <button onClick={()=>client.current.check()}>Повторить проверку</button></p>}
        {section==='structure'?<Structure onUnauthorized={()=>client.current.check()}/>:section==='employees'?<Employees onUnauthorized={()=>client.current.check()}/>:<>
        <div className="eyebrow">Административная панель</div>
        <h1>Добро пожаловать в LMS</h1><p>{auth.user.login}</p><button onClick={()=>client.current.logout()}>Выйти</button>
        <p className="intro">Административная панель корпоративного обучения.</p>
        <section className="panel">
          <h2>Состояние Go API</h2>
          {health.loading ? (
            <p>Проверка соединения…</p>
          ) : ready ? (
            <p className="success">● API работает · PostgreSQL подключён</p>
          ) : (
            <p className="error">● Ошибка проверки API или БД: {health.error || JSON.stringify(health)}</p>
          )}
          <button onClick={checkApi}>Проверить снова</button>

        </section></>}
      </main>
    </div>
  );
}
