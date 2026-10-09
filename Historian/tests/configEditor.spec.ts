import { test, expect } from '@grafana/plugin-e2e';

test('smoke: should render config editor', async ({ createDataSourceConfigPage, readProvisionedDataSource, page }) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await createDataSourceConfigPage({ type: ds.type });
  await expect(page.getByPlaceholder('https://historian:4512/i3x/v1')).toBeVisible();
  await expect(page.getByPlaceholder('https://pulse:4542/auth/token')).toBeVisible();
});

test('"Save & test" should fail and name the missing setting', async ({ createDataSourceConfigPage, page }) => {
  const configPage = await createDataSourceConfigPage({ type: 'flowsoftware-timebasei3x-datasource' });
  await page.getByPlaceholder('https://historian:4512/i3x/v1').fill('https://historian:4512/i3x/v1');
  await expect(configPage.saveAndTest()).not.toBeOK();
  await expect(configPage).toHaveAlert('error', { hasText: 'token URL is required' });
});
