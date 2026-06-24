import { App as AntApp, Form, Input, Modal, Switch } from 'antd';
import { useEffect, useMemo, useState } from 'react';
import { adminApi } from '../../services/adminApi';
import type { Group } from '../../types/api';

type GroupModalMode = 'create' | 'edit';

interface GroupFormData {
  name: string;
  description: string;
  enabled: boolean;
}

interface AccountGroupModalProps {
  open: boolean;
  mode: GroupModalMode;
  group?: Group | null;
  onCancel: () => void;
  onSaved: () => void;
}

const emptyGroupForm: GroupFormData = {
  name: '',
  description: '',
  enabled: true
};

function formFromGroup(group?: Group | null): GroupFormData {
  if (!group) return emptyGroupForm;
  return {
    name: group.name,
    description: group.description || '',
    enabled: group.enabled
  };
}

function normalizeGroupForm(values: GroupFormData) {
  return {
    name: values.name.trim(),
    description: values.description.trim(),
    enabled: values.enabled !== false
  };
}

function hasGroupChanged(originalFormData: GroupFormData, formData: GroupFormData) {
  return JSON.stringify(normalizeGroupForm(originalFormData)) !== JSON.stringify(normalizeGroupForm(formData));
}

export function AccountGroupModal({ open, mode, group, onCancel, onSaved }: AccountGroupModalProps) {
  const { message, modal } = AntApp.useApp();
  const [form] = Form.useForm<GroupFormData>();
  const [formData, setFormData] = useState<GroupFormData>(emptyGroupForm);
  const [originalFormData, setOriginalFormData] = useState<GroupFormData>(emptyGroupForm);
  const [saving, setSaving] = useState(false);

  const editingDefaultGroup = mode === 'edit' && group?.is_default;
  const groupDirty = useMemo(() => hasGroupChanged(originalFormData, formData), [formData, originalFormData]);
  const normalizedForm = normalizeGroupForm(formData);
  const canSave = mode === 'create' ? normalizedForm.name.length > 0 && !saving : groupDirty && normalizedForm.name.length > 0 && !saving;

  useEffect(() => {
    if (!open) return;
    const nextForm = mode === 'edit' ? formFromGroup(group) : emptyGroupForm;
    setOriginalFormData(nextForm);
    setFormData(nextForm);
    form.setFieldsValue(nextForm);
  }, [form, group, mode, open]);

  const closeModal = () => {
    form.resetFields();
    setFormData(emptyGroupForm);
    setOriginalFormData(emptyGroupForm);
    onCancel();
  };

  const requestClose = () => {
    if (!groupDirty) {
      closeModal();
      return;
    }
    modal.confirm({
      title: '关闭账号组弹窗',
      content: '当前有未保存修改，关闭后将丢失，是否继续？',
      okText: '继续关闭',
      cancelText: '继续编辑',
      onOk: closeModal
    });
  };

  const handleSaveGroup = async () => {
    const values = await form.validateFields();
    const normalized = normalizeGroupForm(values);
    setSaving(true);
    try {
      if (mode === 'create') {
        await adminApi.createGroup(normalized);
        message.success('账号组已创建');
      } else if (group) {
        const payload = group.is_default
          ? { description: normalized.description, enabled: normalized.enabled }
          : { name: normalized.name, description: normalized.description, enabled: normalized.enabled };
        await adminApi.updateGroup(group.id, payload);
        message.success('账号组已保存');
      }
      onSaved();
      closeModal();
    } catch (error) {
      message.error(error instanceof Error ? error.message : '保存账号组失败');
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal
      title={mode === 'create' ? '新建账号组' : '编辑账号组'}
      open={open}
      destroyOnClose
      okText="保存"
      cancelText="取消"
      confirmLoading={saving}
      okButtonProps={{ disabled: !canSave }}
      onOk={() => void handleSaveGroup()}
      onCancel={requestClose}
    >
      <Form
        form={form}
        layout="vertical"
        initialValues={emptyGroupForm}
        onValuesChange={(_, values) => setFormData({ ...emptyGroupForm, ...values })}
      >
        <Form.Item
          name="name"
          label="名称"
          rules={[{ required: true, whitespace: true, message: '请输入账号组名称' }]}
          extra={editingDefaultGroup ? '默认组名称不可修改' : undefined}
        >
          <Input disabled={editingDefaultGroup} />
        </Form.Item>
        <Form.Item name="enabled" label="状态" valuePropName="checked">
          <Switch checkedChildren="启用" unCheckedChildren="禁用" disabled={editingDefaultGroup} />
        </Form.Item>
        <Form.Item name="description" label="描述">
          <Input.TextArea rows={4} />
        </Form.Item>
      </Form>
    </Modal>
  );
}
