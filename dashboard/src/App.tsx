import { useEffect, useState } from 'react';
import { BrowserRouter, Navigate, Outlet, Route, Routes } from 'react-router-dom';
import { ConfigProvider, Spin, theme, App as AntApp } from 'antd';
import AppLayout from './components/Layout';
import Login from './pages/Login';
import Overview from './pages/Overview';
import Servers from './pages/Servers';
import ServerDetail from './pages/ServerDetail';
import Characters from './pages/Characters';
import Migrations from './pages/Migrations';
import Operations from './pages/Operations';
import CrossServer from './pages/CrossServer';
import PlayerDiagnose from './pages/PlayerDiagnose';
import SystemConfig from './pages/SystemConfig';
import { probeAdmin } from './api/client';
import { useLang, antdLocale } from './i18n';
import { useTheme } from './theme';

// 会话守卫：进管理台前探一次管理口。401 → 跳登录页；200（已登录或该
// 部署未启用鉴权）→ 放行。只在挂载时探测一次：页内导航不再闪屏，登出/
// 重登会令本组件重新挂载从而复检。
function RequireAuth() {
  const [verdict, setVerdict] = useState<'checking' | 'ok' | 'login'>('checking');
  useEffect(() => {
    probeAdmin().then((v) => setVerdict(v === 'ok' ? 'ok' : 'login'));
  }, []);
  if (verdict === 'checking') {
    return (
      <div style={{ minHeight: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
        <Spin size="large" />
      </div>
    );
  }
  if (verdict === 'login') return <Navigate to="/login" replace />;
  return <Outlet />;
}

export default function App() {
  const lang = useLang();
  const mode = useTheme();

  return (
    <ConfigProvider
      locale={antdLocale()}
      theme={{
        algorithm: mode === 'dark' ? theme.darkAlgorithm : theme.defaultAlgorithm,
        token: {
          colorPrimary: '#d97706',
          borderRadius: 8,
        },
      }}
      key={lang}
    >
      <AntApp>
        <BrowserRouter>
          <Routes>
            <Route path="/login" element={<Login />} />
            <Route element={<RequireAuth />}>
              <Route element={<AppLayout />}>
                <Route path="/" element={<Overview />} />
                <Route path="/servers" element={<Servers />} />
                <Route path="/servers/:id" element={<ServerDetail />} />
                <Route path="/characters" element={<Characters />} />
                <Route path="/migrations" element={<Migrations />} />
                <Route path="/operations" element={<Operations />} />
                <Route path="/crossserver" element={<CrossServer />} />
                <Route path="/diagnose" element={<PlayerDiagnose />} />
                <Route path="/system" element={<SystemConfig />} />
              </Route>
            </Route>
          </Routes>
        </BrowserRouter>
      </AntApp>
    </ConfigProvider>
  );
}
