import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const sourcePath = resolve(process.cwd(), 'src/views/admin/AIRelayView.vue')

describe('AI Relay administration view', () => {
  it('reuses the account management table primitives', () => {
    const source = readFileSync(sourcePath, 'utf8')

    expect(source).toContain("@/components/layout/TablePageLayout.vue")
    expect(source).toContain("@/components/common/DataTable.vue")
    expect(source).toContain("@/components/common/Pagination.vue")
    expect(source).toContain("buildGatewayUrl('/plugins/ai-relay/api')")
    expect(source).toContain('@update:pageSize="changePageSize"')
    expect(source).toContain("const fallbackPlatforms: Platform[] = [{ key: 'agnes', display_name: 'Agnes' }]")
  })
})
