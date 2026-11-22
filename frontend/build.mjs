// Compile Vue templates ahead of time so the receiver needs neither Node nor eval at runtime.
import { copyFile, mkdir, readFile, writeFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import { resolve } from 'node:path'
import { parse, compileScript } from 'vue/compiler-sfc'

process.chdir(fileURLToPath(new URL('.', import.meta.url)))
const target = resolve('../internal/receiver/assets/ui')
await mkdir(target, { recursive: true })
for (const name of ['PublicStatus', 'Management']) {
  const filename = `src/${name}.vue`
  const { descriptor, errors } = parse(await readFile(filename, 'utf8'), { filename })
  if (errors.length) throw errors[0]
  const { content } = compileScript(descriptor, {
    id: name,
    inlineTemplate: true,
    isProd: true,
    templateOptions: { compilerOptions: { runtimeModuleName: './vue.js' } },
  })
  await writeFile(
    resolve(target, name + '.js'),
    content.replaceAll(/from (['"])vue\1/g, "from './vue.js'"),
  )
}
const files = {
  'src/main.js': 'app.js',
  'src/bridge.svg': 'bridge.svg',
  'node_modules/vue/dist/vue.runtime.esm-browser.prod.js': 'vue.js',
}
const licenses = ['Zenon-LICENSE.txt', 'node_modules/vue/LICENSE']
for (const name of ['space-grotesk', 'jetbrains-mono']) {
  const pkg = `node_modules/@fontsource-variable/${name}`
  const font = `${name}-latin-wght-normal.woff2`
  files[`${pkg}/files/${font}`] = font
  licenses.push(pkg + '/LICENSE')
}
for (const [source, name] of Object.entries(files)) {
  await copyFile(source, resolve(target, name))
}
const styles = await Promise.all(
  ['src/zenon-colors.css', 'src/style.css'].map((path) => readFile(path, 'utf8')),
)
await writeFile(resolve(target, 'app.css'), styles.join('\n'))
const notices = await Promise.all(licenses.map((path) => readFile(path, 'utf8')))
await writeFile(resolve(target, 'licenses.txt'), notices.join('\n\n'))
