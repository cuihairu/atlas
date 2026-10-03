import { useState } from 'react';
import { Alert, Button, Card, Form, Input } from 'antd';
import { LockOutlined, UserOutlined } from '@ant-design/icons';
import { useNavigate } from 'react-router-dom';
import { probeAdmin, setAdminSession } from '../api/client';
import { t } from '../i18n';
import { useTheme } from '../theme';

// 管理台登录：后端鉴权是 Bearer API Key（按角色授权，演示账号为 viewer
// 只读），表单里的"密码"就是该账号的密钥——通过后才落会话。
export default function Login() {
  const navigate = useNavigate();
  const mode = useTheme();
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  const onFinish = async (values: { account: string; password: string }) => {
    setBusy(true);
    setError('');
    const verdict = await probeAdmin(values.password);
    if (verdict === 'ok') {
      setAdminSession(values.account.trim() || 'demo', values.password);
      navigate('/', { replace: true });
    } else {
      setError(t('loginFailed'));
    }
    setBusy(false);
  };

  return (
    <div
      style={{
        minHeight: '100vh',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        background: mode === 'dark' ? '#141414' : '#f5f5f5',
        padding: 16,
      }}
    >
      <Card style={{ width: 380 }} styles={{ body: { padding: '32px 32px 24px' } }}>
        <div
          style={{
            textAlign: 'center',
            color: '#d97706',
            fontWeight: 700,
            fontSize: 28,
            letterSpacing: 4,
            marginBottom: 4,
          }}
        >
          ATLAS
        </div>
        <div style={{ textAlign: 'center', opacity: 0.65, marginBottom: 24, fontSize: 13 }}>
          {t('loginSubtitle')}
        </div>
        <Form<{ account: string; password: string }> onFinish={onFinish} layout="vertical">
          <Form.Item
            name="account"
            rules={[{ required: true }]}
          >
            <Input
              prefix={<UserOutlined />}
              placeholder={t('account')}
              autoComplete="username"
              size="large"
            />
          </Form.Item>
          <Form.Item
            name="password"
            rules={[{ required: true }]}
          >
            <Input.Password
              prefix={<LockOutlined />}
              placeholder={t('password')}
              autoComplete="current-password"
              size="large"
            />
          </Form.Item>
          {error && (
            <Alert
              type="error"
              message={error}
              showIcon
              style={{ marginBottom: 16 }}
            />
          )}
          <Button type="primary" htmlType="submit" block size="large" loading={busy}>
            {t('login')}
          </Button>
        </Form>
        <Alert
          type="info"
          showIcon={false}
          message={t('demoHint')}
          style={{ marginTop: 20, fontSize: 12 }}
        />
      </Card>
    </div>
  );
}
