import { useEffect, useState } from 'react';
import { BrowserRouter, Navigate, Outlet, Route, Routes, useNavigate } from 'react-router-dom';
import { Button, ConfigProvider, Result, Spin, theme, App as AntApp } from 'antd';
import AppLayout from './components/Layout';
import Login from './pages/Login';
import Overview from './pages/Overview';
import Servers from './pages/Servers';
import Heatmap from './pages/Heatmap';
import ServerDetail from './pages/ServerDetail';
import Characters from './pages/Characters';
import Migrations from './pages/Migrations';
import Operations from './pages/Operations';
import CrossServer from './pages/CrossServer';
import PlayerDiagnose from './pages/PlayerDiagnose';
import SystemConfig from './pages/SystemConfig';
import AuditLog from './pages/AuditLog';
import { probeAdmin } from './api/client';
import { setAppMessage } from './api/messageBus';
import { useLang, antdLocale, t } from './i18n';
import { useTheme } from './theme';

// Session guard: probe the admin endpoint once before entering the console.
// 401 → go to the login page; 200 (already logged in, or the deployment has
// auth disabled) → let through. Probed only once on mount: in-app navigation
// no longer flashes, and logout/re-login remounts this component so the
// check runs again.
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

// Inject the App.useApp() message instance through the holder for
// non-component modules (api/client.ts); removes antd's warning about the
// theme-unaware static message and the dark-mode style mismatch.
function MessageBridge() {
  const { message } = AntApp.useApp();
  useEffect(() => {
    setAppMessage(message);
    return () => setAppMessage(null);
  }, [message]);
  return null;
}

// Fallback for unmatched routes: no longer silently renders an empty content
// area (previously seen on the demo site when visiting /heatmap directly).
function NotFound() {
  const navigate = useNavigate();
  return (
    <Result
      status="404"
      title="404"
      subTitle={t('notFound')}
      extra={
        <Button type="primary" onClick={() => navigate('/')}>
          {t('backHome')}
        </Button>
      }
    />
  );
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
        <MessageBridge />
        <BrowserRouter>
          <Routes>
            <Route path="/login" element={<Login />} />
            <Route element={<RequireAuth />}>
              <Route element={<AppLayout />}>
                <Route path="/" element={<Overview />} />
                <Route path="/servers" element={<Servers />} />
                <Route path="/servers/:id" element={<ServerDetail />} />
                <Route path="/heatmap" element={<Heatmap />} />
                <Route path="/characters" element={<Characters />} />
                <Route path="/migrations" element={<Migrations />} />
                <Route path="/operations" element={<Operations />} />
                <Route path="/crossserver" element={<CrossServer />} />
                <Route path="/diagnose" element={<PlayerDiagnose />} />
                <Route path="/system" element={<SystemConfig />} />
                <Route path="/audit" element={<AuditLog />} />
                <Route path="*" element={<NotFound />} />
              </Route>
            </Route>
          </Routes>
        </BrowserRouter>
      </AntApp>
    </ConfigProvider>
  );
}
