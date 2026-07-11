import { mkdir } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { resolve } from 'node:path'
import { chromium } from 'playwright-core'

const baseURL = process.env.PLUGIN_URL || 'http://127.0.0.1:8091/plugins/image-generation'
const screenshotDir = resolve(process.env.SCREENSHOT_DIR || tmpdir(), 'sub2api-image-generation-smoke')
const executablePath = process.env.EDGE_PATH || 'C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe'
await mkdir(screenshotDir, { recursive: true })

const browser = await chromium.launch({ executablePath, headless: true })
const errors = []

async function preparePage(viewport) {
  const page = await browser.newPage({ viewport })
  page.on('console', message => { if (message.type() === 'error') errors.push(message.text()) })
  page.on('pageerror', error => errors.push(error.message))
  await page.route('**/logo.png', route => route.fulfill({ status: 204 }))
  await page.route('**/api/v1/keys?**', route => route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({ code: 0, data: { items: [{
      id: 1, key: 'sk-test', name: 'Browser test key', status: 'active',
      group: { allow_image_generation: true, models_list_config: { enabled: true, models: ['gpt-image-2'] } },
    }] } }),
  }))
  await page.route('**/plugins/image-generation/api/**', async route => {
    const url = new URL(route.request().url())
    if (url.pathname.includes('/assets/')) return route.fulfill({
      contentType: 'image/png',
      body: Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9Z5ZkAAAAASUVORK5CYII=', 'base64'),
    })
    if (url.pathname.endsWith('/conversations')) return route.fulfill({ json: { items: [] } })
    if (url.pathname.includes('/conversations/') && url.pathname.endsWith('/messages')) return route.fulfill({ json: { items: [] } })
    if (url.pathname.endsWith('/generate')) return route.fulfill({ status: 201, json: {
      job_id: 'browser-job', status: 'succeeded',
      result: { images: [{
        url: '/plugins/image-generation/api/assets/browser-job/result/0',
        preview_url: '/plugins/image-generation/api/assets/browser-job/result/0/preview',
        revised_prompt: 'Browser smoke result',
      }] },
    } })
    return route.fulfill({ status: 404, json: { error: 'not found' } })
  })
  return page
}

try {
  const desktop = await preparePage({ width: 1440, height: 900 })
  await desktop.goto(baseURL, { waitUntil: 'networkidle' })
  await desktop.getByTestId('reference-image-input').setInputFiles({
    name: 'reference.png',
    mimeType: 'image/png',
    buffer: Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9Z5ZkAAAAASUVORK5CYII=', 'base64'),
  })
  await desktop.getByTestId('image-prompt-input').fill('Create a browser smoke image')
  await desktop.getByTestId('image-send-button').click()
  await desktop.getByTestId('message-attachments').getByText('Browser smoke result').waitFor()
  const visualContract = await desktop.evaluate(() => {
    const style = (selector) => getComputedStyle(document.querySelector(selector))
    return {
      bodyBackground: getComputedStyle(document.body).backgroundColor,
      historyBackground: style('[data-testid="image-history"]').backgroundColor,
      historyWidth: style('[data-testid="image-history"]').width,
      composerRadius: style('[data-testid="image-chat-composer"]').borderRadius,
      assistantRadius: style('.message-assistant .message-body').borderRadius,
      chatBackground: style('[data-testid="image-chat-panel"]').backgroundColor,
    }
  })
  const expectedVisualContract = {
    bodyBackground: 'rgb(248, 246, 241)',
    historyBackground: 'rgb(251, 251, 248)',
    historyWidth: '300px',
    composerRadius: '28px',
    assistantRadius: '24px',
    chatBackground: 'rgb(248, 246, 241)',
  }
  if (JSON.stringify(visualContract) !== JSON.stringify(expectedVisualContract)) {
    throw new Error(`Visual contract mismatch: ${JSON.stringify(visualContract)}`)
  }
  await desktop.getByTestId('history-inline-collapse').click()
  await desktop.getByTestId('history-drawer-toggle').waitFor({ state: 'visible' })
  const collapsedRight = await desktop.getByTestId('image-history').evaluate(element => element.getBoundingClientRect().right)
  if (collapsedRight > 0) throw new Error('Desktop history sidebar did not collapse')
  await desktop.getByTestId('history-drawer-toggle').click()
  await desktop.getByTestId('image-history').waitFor({ state: 'visible' })
  const desktopLayout = await desktop.evaluate(() => {
    const composer = document.querySelector('[data-testid="image-chat-composer"]')?.getBoundingClientRect()
    const singleImageBubble = document.querySelector('.message-assistant .message-body:has(.image-grid.single-image)')?.getBoundingClientRect()
    const actionTops = Array.from(document.querySelectorAll('.image-actions button')).map(button => button.getBoundingClientRect().top)
    const userBubble = document.querySelector('.message-user .message-body')?.getBoundingClientRect()
    const userText = document.querySelector('.message-user')?.textContent || ''
    const parameterPills = document.querySelectorAll('.message-user .request-settings span').length
    return composer && composer.left >= 0 && composer.right <= innerWidth && composer.bottom <= innerHeight
      && singleImageBubble && singleImageBubble.width <= 400
      && actionTops.length === 4 && new Set(actionTops.map(top => Math.round(top))).size === 1
      && userBubble && userBubble.width <= 320
      && userText.includes('Prompt') && userText.includes('创作描述') && userText.includes('生成参数')
      && parameterPills === 1
  })
  if (!desktopLayout) throw new Error('Desktop composer exceeds the viewport')
  await desktop.getByRole('button', { name: '查看原图' }).first().click()
  await desktop.getByRole('dialog', { name: '查看原图' }).waitFor()
  const downloadHref = await desktop.getByRole('link', { name: '下载原图' }).getAttribute('href')
  if (!downloadHref?.includes('download=1')) throw new Error('Original download URL is missing download=1')
  await desktop.getByRole('button', { name: '关闭原图' }).click()
  await desktop.screenshot({ path: resolve(screenshotDir, 'image-generation-desktop.png'), fullPage: true })
  await desktop.close()

  const mobile = await preparePage({ width: 390, height: 844 })
  await mobile.goto(baseURL, { waitUntil: 'networkidle' })
  await mobile.getByTestId('history-drawer-toggle').click()
  await mobile.getByTestId('image-key-select').waitFor({ state: 'visible' })
  await mobile.waitForTimeout(250)
  const mobileLayout = await mobile.evaluate(() => {
    const sidebar = document.querySelector('.sidebar-wrap')?.getBoundingClientRect()
    return document.documentElement.scrollWidth <= innerWidth && sidebar && sidebar.left >= 0 && sidebar.right > 0
  })
  if (!mobileLayout) throw new Error('Mobile page has horizontal overflow')
  await mobile.screenshot({ path: resolve(screenshotDir, 'image-generation-mobile.png'), fullPage: true })
  await mobile.close()

  if (errors.length) throw new Error(`Browser console errors:\n${errors.join('\n')}`)
  console.log(`Browser smoke test passed. Screenshots: ${screenshotDir}`)
} finally {
  await browser.close()
}
