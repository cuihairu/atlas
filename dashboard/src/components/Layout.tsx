import { useState } from 'react';
import { Layout as AntLayout, Menu, Button, Tooltip, theme } from 'antd';
import {
  DashboardOutlined,
  CloudServerOutlined,
  UserOutlined,
  SwapOutlined,
  NotificationOutlined,
  ClusterOutlined,
  SunOutlined,
  MoonOutlined,
  GlobalOutlined,
  LogoutOutlined,
} from '@ant-design/icons';
import { Outlet, useNavigate, useLocation } from 'react-router-dom';
import { useLang, setLang, t, type Lang } from '../i18n';
import { useTheme, setTheme } from '../theme';
import { clearAdminSession, getAdminAccount } from '../api/client';

const { Sider, Header, Content } = AntLayout;

export default function AppLayout() {
  const [collapsed, setCollapsed] = useState(false);
  const navigate = useNavigate();
  const location = useLocation();
  const lang = useLang();
  const mode = useTheme();
  const account = getAdminAccount();
  const {
    token: { colorBgContainer, colorBorder, borderRadiusLG },
  } = theme.useToken();

  const MENU_ITEMS = [
    { key: '/', icon: <DashboardOutlined />, label: t('overview') },
    { key: '/servers', icon: <CloudServerOutlined />, label: t('servers') },
    { key: '/characters', icon: <UserOutlined />, label: t('characterSearch') },
    { key: '/migrations', icon: <SwapOutlined />, label: t('migrations') },
    { key: '/operations', icon: <NotificationOutlined />, label: t('operations') },
    { key: '/crossserver', icon: <ClusterOutlined />, label: t('crossserverConfig') },
  ];

  // Determine selected key from path
  const selectedKey =
    MENU_ITEMS.find(
      (item) => item.key !== '/' && location.pathname.startsWith(item.key),
    )?.key ?? '/';

  return (
    <AntLayout style={{ minHeight: '100vh' }}>
      <Sider
        collapsible
        collapsed={collapsed}
        onCollapse={setCollapsed}
        theme={mode === 'dark' ? 'dark' : 'light'}
        style={{ background: mode === 'dark' ? '#141414' : colorBgContainer, borderBottom: `1px solid ${colorBorder}` }}
      >
        <div
          style={{
            height: 48,
            margin: 16,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            color: '#d97706',
            fontWeight: 700,
            fontSize: collapsed ? 18 : 20,
            letterSpacing: 2,
            whiteSpace: 'nowrap',
            overflow: 'hidden',
          }}
        >
          {collapsed ? 'AT' : 'ATLAS'}
        </div>
        <Menu
          theme={mode === 'dark' ? 'dark' : 'light'}
          mode="inline"
          selectedKeys={[selectedKey]}
          items={MENU_ITEMS}
          onClick={({ key }) => navigate(key)}
          style={{ background: 'transparent', borderRight: 0 }}
        />
      </Sider>
      <AntLayout>
        <Header
          style={{
            padding: '0 24px',
            background: colorBgContainer,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            fontSize: 18,
            fontWeight: 600,
          }}
        >
          <span>Atlas Dashboard</span>
          <span style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
            {account && (
              <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6, fontSize: 13, fontWeight: 500, opacity: 0.75 }}>
                <UserOutlined /> {account}
              </span>
            )}
            {account && (
              <Tooltip title={t('logout')}>
                <Button
                  icon={<LogoutOutlined />}
                  size="small"
                  aria-label={t('logout')}
                  onClick={() => {
                    clearAdminSession();
                    navigate('/login');
                  }}
                />
              </Tooltip>
            )}
            <Tooltip title={`${t('language')}: ${lang === 'zh' ? '中文' : 'English'}`}>
              <Button
                icon={<GlobalOutlined />}
                size="small"
                onClick={() => setLang((lang === 'zh' ? 'en' : 'zh') as Lang)}
              >
                {lang === 'zh' ? '中文' : 'EN'}
              </Button>
            </Tooltip>
            <Tooltip title={t('theme')}>
              <Button
                icon={mode === 'dark' ? <SunOutlined /> : <MoonOutlined />}
                size="small"
                onClick={() => setTheme(mode === 'dark' ? 'light' : 'dark')}
                aria-label="toggle theme"
              />
            </Tooltip>
          </span>
        </Header>
        <Content
          style={{
            margin: 24,
            padding: 24,
            background: colorBgContainer,
            borderRadius: borderRadiusLG,
            minHeight: 360,
          }}
        >
          <Outlet />
        </Content>
      </AntLayout>
    </AntLayout>
  );
}
