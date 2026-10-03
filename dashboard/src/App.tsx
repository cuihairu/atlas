import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { ConfigProvider, theme, App as AntApp } from 'antd';
import AppLayout from './components/Layout';
import Overview from './pages/Overview';
import Servers from './pages/Servers';
import ServerDetail from './pages/ServerDetail';
import Characters from './pages/Characters';
import Migrations from './pages/Migrations';
import Operations from './pages/Operations';
import CrossServer from './pages/CrossServer';
import { useLang, antdLocale } from './i18n';
import { useTheme } from './theme';

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
            <Route element={<AppLayout />}>
              <Route path="/" element={<Overview />} />
              <Route path="/servers" element={<Servers />} />
              <Route path="/servers/:id" element={<ServerDetail />} />
              <Route path="/characters" element={<Characters />} />
              <Route path="/migrations" element={<Migrations />} />
              <Route path="/operations" element={<Operations />} />
              <Route path="/crossserver" element={<CrossServer />} />
            </Route>
          </Routes>
        </BrowserRouter>
      </AntApp>
    </ConfigProvider>
  );
}
