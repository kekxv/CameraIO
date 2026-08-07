import assert from 'node:assert/strict'
import test from 'node:test'

globalThis.localStorage = {
  getItem: () => null,
  removeItem: () => {},
}

const { default: api, captureSnapshot } = await import('./api.js')

test('captureSnapshot requests the native JPEG endpoint as a blob', async () => {
  const originalAdapter = api.defaults.adapter
  let request
  api.defaults.adapter = async (config) => {
    request = config
    return {
      data: new Blob(['jpeg'], { type: 'image/jpeg' }),
      status: 200,
      statusText: 'OK',
      headers: { 'content-type': 'image/jpeg' },
      config,
    }
  }

  try {
    const snapshot = await captureSnapshot(12)
    assert.equal(request.url, '/cameras/12/snapshot')
    assert.equal(request.method, 'get')
    assert.equal(request.responseType, 'blob')
    assert.equal(await snapshot.text(), 'jpeg')
  } finally {
    api.defaults.adapter = originalAdapter
  }
})
