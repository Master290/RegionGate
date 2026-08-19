const mineflayer = require('mineflayer')

const host = process.env.REGIONGATE_SMOKE_HOST || '127.0.0.1'
const port = Number(process.env.REGIONGATE_SMOKE_PORT || '25565')
const count = Number(process.env.REGIONGATE_SMOKE_CLIENTS || '3')
const holdMs = Number(process.env.REGIONGATE_SMOKE_HOLD_MS || '45000')

if (!Number.isInteger(count) || count < 1 || count > 16) {
  throw new Error('REGIONGATE_SMOKE_CLIENTS must be an integer from 1 to 16')
}

const bots = []
let spawned = 0
let failed = false
let finishing = false

function finish(code) {
  if (finishing) return
  finishing = true
  for (const bot of bots) bot.quit('Smoke test complete')
  setTimeout(() => process.exit(code), 500)
}

for (let index = 1; index <= count; index++) {
  const username = `RGSmoke${index}`
  const bot = mineflayer.createBot({
    host,
    port,
    username,
    version: '1.20.4',
    auth: 'offline'
  })
  bots.push(bot)

  bot.once('spawn', () => {
    spawned++
    console.log(`${username}: spawned (${spawned}/${count})`)
    if (spawned === count) {
      console.log(`all clients spawned; holding for ${holdMs}ms`)
      setTimeout(() => finish(failed ? 1 : 0), holdMs)
    }
  })

  bot.on('kicked', reason => {
    failed = true
    console.error(`${username}: kicked: ${reason}`)
    finish(1)
  })

  bot.on('error', error => {
    failed = true
    console.error(`${username}: error: ${error.message}`)
    finish(1)
  })

  bot.on('end', reason => {
    if (!finishing) {
      failed = true
      console.error(`${username}: disconnected early: ${reason}`)
      finish(1)
    }
  })
}

setTimeout(() => {
  if (spawned !== count) {
    console.error(`startup timeout: spawned ${spawned}/${count}`)
    finish(1)
  }
}, 30000)
