import { useState } from 'react';
import { Layout as AntLayout, Menu, theme } from 'antd';
import {
  DashboardOutlined,
  CloudServerOutlined,
  UserOutlined,
  SwapOutlined,
} from '@ant-design/icons';
import { Outlet, useNavigate, useLocation } from 'react-router-dom';

const { Sider, Header, Content } = AntLayout;

const MENU_ITEMS = [
  { key: '/', icon: <DashboardOutlined />, label: '总览' },
  { key: '/servers', icon: <CloudServerOutlined />, label: '服务器' },
  { key: '/characters', icon: <UserOutlined />, label: '角色搜索' },
  { key: '/migrations', icon: <SwapOutlined />, label: '迁移管理' },
];

export default function AppLayout() {
  const [collapsed, setCollapsed] = useState(false);
  const navigate = useNavigate();
  const location = useLocation();
  const {
    token: { colorBgContainer, borderRadiusLG },
  } = theme.useToken();

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
        style={{ background: '#141414' }}
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
          theme="dark"
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
            fontSize: 18,
            fontWeight: 600,
          }}
        >
          Atlas Dashboard
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