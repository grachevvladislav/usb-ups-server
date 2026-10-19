/* Native DSM 7 application. DSM supplies Vue and the v-* components.
 * No iframe, standalone page, template compiler, or separate login is used.
 */
(function () {
    'use strict';
    var API = '/webman/3rdparty/usb-ups-server/app/api/';
    var fields = ['driver', 'subdriver', 'vendor_id', 'product_id', 'serial', 'listen', 'port', 'poll_interval', 'enabled'];
    var readings = [
        ['input.voltage', 'Входное напряжение', 'В'], ['output.voltage', 'Выходное напряжение', 'В'],
        ['ups.load', 'Нагрузка', '%'], ['battery.charge', 'Заряд батареи', '%'],
        ['battery.voltage', 'Напряжение батареи', 'В'], ['battery.runtime', 'Время работы от батареи', 'сек'],
        ['ups.temperature', 'Температура', '°C']
    ];
    function options(values) { return values.map(function (x) { return { label: x || 'Автоматически', value: x }; }); }
    SYNO.namespace('org.usbupsserver');
    org.usbupsserver.App = Vue.extend({
        name: 'USBUPSServer',
        data: function () {
            return { tab: 'status', loaded: false, busy: false, message: '', error: '', session: {}, synoToken: '', permissionHelp: null, suggestedListen: '',
                cfg: {}, drivers: [], subdrivers: [], devices: [], selectedUSB: '', live: {}, logText: '' };
        },
        created: function () {
            var self = this;
            this.initialize();
            this.pollTimer = setInterval(function () {
                if (self.loaded && !self.busy && self.tab === 'status') self.refreshStatus();
            }, 5000);
        },
        beforeDestroy: function () { clearInterval(this.pollTimer); },
        methods: {
            api: async function (name, body) {
                var request = { credentials: 'same-origin', headers: { 'X-UPS-Request': '1', 'X-SYNO-TOKEN': this.synoToken } };
                if (body !== undefined) {
                    request.method = 'POST';
                    request.headers['Content-Type'] = 'application/json';
                    request.body = JSON.stringify(body);
                }
                var response = await fetch(API + name, request);
                var data = await response.json();
                if (!response.ok) { if (data.code === 'usb_permission_denied') this.permissionHelp = data; throw Error(data.error || ('Ошибка HTTP ' + response.status)); }
                return data;
            },
            initialize: async function () {
                if (this.busy) return;
                this.busy = true; this.error = '';
                try {
                    var login = await fetch('/webman/login.cgi', { credentials: 'same-origin', cache: 'no-store' });
                    var identity = await login.json();
                    if (!login.ok || typeof identity.SynoToken !== 'string' || !identity.SynoToken)
                        throw Error('Сессия DSM истекла. Войдите в DSM и откройте приложение снова.');
                    this.synoToken = identity.SynoToken;
                    this.session = await this.api('session');
                    var result = await Promise.all([this.api('settings'), this.api('usb'), this.api('status'), this.api('logs')]);
                    this.cfg = Object.assign({}, result[0].config);
                    this.suggestedListen = result[0].suggested_listen || '';
                    if (result[0].listen_autodetected) this.message = 'Подставлен LAN-IP NAS: ' + this.cfg.listen + '. Для подключения DSM нажмите «Применить» и укажите этот же IP в настройках ИБП DSM.';
                    this.drivers = result[0].drivers; this.subdrivers = result[0].subdrivers;
                    this.devices = result[1]; this.syncUSBSelection(); this.live = result[2]; this.logText = result[3].text;
                    this.loaded = true;
                } catch (e) { this.error = e.message; }
                finally { this.busy = false; }
            },
            refreshStatus: async function () {
                try { this.live = await this.api('status'); }
                catch (e) { this.live = { ok: false, state: 'api_unavailable', health: this.live.health || { enabled: !!this.cfg.enabled }, error: 'Нет связи с приложением. Проверьте его состояние в Центре пакетов DSM.' }; }
            },
            testNotification: async function () {
                if (this.busy) return;
                this.busy = true; this.error = ''; this.message = '';
                try { await this.api('test-notification', {}); this.message = 'Запрос отправлен DSM. Проверьте уведомление в правом верхнем углу рабочего стола.'; }
                catch (e) { this.error = 'Не удалось отправить уведомление DSM: ' + e.message; }
                finally { this.busy = false; }
            },
            refreshUSB: async function () {
                try { this.devices = await this.api('usb'); this.syncUSBSelection(); this.error = ''; }
                catch (e) { this.error = e.message; }
            },
            refreshLogs: async function () {
                try { this.logText = (await this.api('logs')).text; this.error = ''; }
                catch (e) { this.error = e.message; }
            },
            navigate: function (tab) {
                this.tab = tab;
                if (tab === 'status') this.refreshStatus();
                if (tab === 'logs') this.refreshLogs();
            },
            syncUSBSelection: function () {
                var cfg = this.cfg, matches = [];
                this.devices.forEach(function (device, i) {
                    if (String(device.vendor_id).toLowerCase() === String(cfg.vendor_id || '').toLowerCase() &&
                        String(device.product_id).toLowerCase() === String(cfg.product_id || '').toLowerCase() &&
                        (!cfg.serial || device.serial === cfg.serial)) matches.push(String(i));
                });
                this.selectedUSB = matches.length === 1 ? matches[0] : cfg.vendor_id && cfg.product_id ? 'configured' : '';
                this.checkPermissions();
            },
            checkPermissions: function () {
                var cfg = this.cfg;
                if (!/^[a-f0-9]{4}$/i.test(cfg.vendor_id || '') || !/^[a-f0-9]{4}$/i.test(cfg.product_id || '')) return true;
                var matches = this.devices.filter(function (d) { return String(d.vendor_id).toLowerCase() === cfg.vendor_id.toLowerCase() && String(d.product_id).toLowerCase() === cfg.product_id.toLowerCase() && (!cfg.serial || cfg.serial === d.serial); });
                if (matches.length && matches.every(function (d) { return d.access === 'denied'; })) {
                    this.permissionHelp = { error: 'Нет доступа к USB-устройству ' + cfg.vendor_id + ':' + cfg.product_id,
                        command: 'sudo sh /var/packages/usb-ups-server/target/tools/usb-access.sh install-rule ' + cfg.vendor_id.toLowerCase() + ' ' + cfg.product_id.toLowerCase(),
                        hint: 'Выполните команду в своей SSH-сессии на NAS. Затем переподключите USB-кабель ИБП и обновите список. Питание ИБП отключать не нужно.' };
                    return false;
                }
                this.permissionHelp = null;
                return true;
            },
            selectUSB: function (value) {
                this.selectedUSB = value;
                var d = this.devices[Number(value)];
                if (value === '' || !d) return;
                this.$set(this.cfg, 'vendor_id', d.vendor_id);
                this.$set(this.cfg, 'product_id', d.product_id);
                this.$set(this.cfg, 'serial', d.serial || '');
                if (d.vendor_id.toLowerCase() === '0925' && d.product_id.toLowerCase() === '1234') {
                    this.$set(this.cfg, 'driver', 'nutdrv_qx'); this.$set(this.cfg, 'subdriver', 'armac');
                    this.$set(this.cfg, 'serial', '');
                    this.message = 'Выбран профиль RICHCOMM / Armac.';
                }
                this.checkPermissions();
            },
            payload: function () {
                var self = this, result = {};
                fields.forEach(function (key) {
                    result[key] = key === 'enabled' ? !!self.cfg[key] :
                        ['port', 'poll_interval'].indexOf(key) >= 0 ? Number(self.cfg[key]) : String(self.cfg[key] || '').trim();
                });
                return result;
            },
            apply: async function () {
                if (this.busy || (this.cfg.enabled && !this.checkPermissions())) return;
                this.busy = true; this.error = ''; this.message = 'Проверка подключения…';
                try {
                    await this.api('settings', this.payload());
                    this.message = 'Настройки сохранены.';
                    await this.refreshStatus();
                } catch (e) { this.message = ''; this.error = e.message; }
                finally { this.busy = false; }
            },
            detect: async function () {
                if (this.busy || this.cfg.enabled || !this.checkPermissions()) return;
                this.busy = true; this.error = ''; this.message = 'Подбор драйвера… До 5 минут.';
                try {
                    this.cfg = await this.api('detect', this.payload()); this.syncUSBSelection();
                    this.message = 'Драйвер найден. Включите мониторинг и примените настройки.';
                } catch (e) { this.message = ''; this.error = e.message; }
                finally { this.busy = false; }
            },
            download: async function () {
                try {
                    var data = await this.api('diagnostics');
                    var url = URL.createObjectURL(new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' }));
                    var anchor = document.createElement('a');
                    anchor.href = url; anchor.download = 'ups-diagnostics.json'; anchor.click();
                    setTimeout(function () { URL.revokeObjectURL(url); }, 1000);
                } catch (e) { this.error = e.message; }
            }
        },
        render: function (h) {
            var self = this;
            function button(label, click, primary, disabled) {
                return h('v-button', { props: { suffix: primary ? 'main' : 'grey', disabled: !!disabled }, on: { click: click } }, [label]);
            }
            function formItem(label, content, description) { return h('v-form-item', { props: { label: label, labelFlex: '210px' } }, [content, description ? h('span', { slot: 'description', style: { overflowWrap: 'anywhere' } }, [description]) : null]); }
            function input(key, label, numeric) {
                return formItem(label, h('v-input', { style: { width: '100%', maxWidth: '100%' },
                    props: { disabled: self.busy, numberOnly: !!numeric },
                    model: { value: self.cfg[key], callback: function (value) { self.$set(self.cfg, key, value); } } }));
            }
            function select(key, label, values) {
                return formItem(label, h('v-single-select', { props: { options: values, width: '100%', disabled: self.busy, editable: false, clearable: false },
                    model: { value: self.cfg[key], callback: function (value) { self.$set(self.cfg, key, value); } } }));
            }
            function actions(children) { return h('div', { style: { display: 'flex', flexWrap: 'wrap', gap: '8px', margin: '12px 0' } }, children); }
            function heading(text) { return h('h2', { class: 'ups-section-title' }, [text]); }
            function note(text) { return h('p', { style: { margin: '12px 0', color: '#707780', lineHeight: '1.6' } }, [text]); }
            function table(rows) {
                return h('table', { style: { width: '100%', tableLayout: 'fixed', borderCollapse: 'collapse' } }, rows.map(function (row) {
                    return h('tr', row.map(function (value) { return h('td', { style: { padding: '8px 12px', borderBottom: '1px solid #e6e8eb', overflowWrap: 'anywhere' } }, [String(value)]); }));
                }));
            }
            var body = [];
            if (!this.loaded) {
                body = [heading(this.busy ? 'Загрузка…' : 'Нет подключения'), note(this.error || 'Проверка текущей сессии DSM…'), button('Повторить', this.initialize, false, this.busy)];
            } else if (this.tab === 'status') {
                var vars = this.live.variables || {}, health = this.live.health || {}, state = String(vars['ups.status'] || '').split(/\s+/);
                var title = this.live.state === 'api_unavailable' ? 'Приложение недоступно' : !health.enabled ? 'Мониторинг выключен' : this.live.state === 'usb_disconnected' ? 'ИБП отключён от USB' : this.live.state === 'usb_permission_denied' ? 'Нет доступа к USB' : !this.live.ok ? 'Восстановление связи с ИБП' :
                    state.indexOf('LB') >= 0 ? 'Низкий заряд батареи' : state.indexOf('OB') >= 0 ? 'Работа от батареи' :
                    state.indexOf('OL') >= 0 ? 'Питание от сети' : vars['ups.status'];
                body.push(heading(title), note(this.live.error || 'Драйвер: ' + (health.driver_running ? 'работает' : 'остановлен') + ' · NUT: ' + (health.server_running ? 'работает' : 'остановлен')));
                if (health.enabled && !this.live.ok && this.live.state !== 'api_unavailable') body.push(note('Мониторинг остаётся включённым. Повторные попытки выполняются автоматически; переключать галочку не нужно.'));
                if (health.notification_error) body.push(note('Не удалось доставить уведомление DSM: ' + health.notification_error));
                var observed = this.live.observed_at;
                if (observed) body.push(note('Показания получены: ' + new Date(observed).toLocaleTimeString()));
                body.push(h('v-form', { props: { direction: 'horizontal' } }, readings.filter(function (r) { return self.live.ok && vars[r[0]] !== undefined; }).map(function (r) {
                    return formItem(r[1], h('span', [vars[r[0]] + ' ' + r[2]]));
                })));
                if (this.live.ok && Object.keys(vars).length) body.push(h('details', { style: { marginTop: '24px' } }, [h('summary', ['Все переменные NUT']), table(Object.keys(vars).sort().map(function (key) { return [key, vars[key]]; }))]));
                body.push(note('Переход в безопасное состояние настраивается в «Панель управления → Оборудование и питание → ИБП». Испытание завершения работы NAS ещё не проведено.'));
            } else if (this.tab === 'settings') {
                var chosenDevice = this.devices[Number(this.selectedUSB)];
                var usbDescription = chosenDevice && this.selectedUSB !== '' ? (chosenDevice.name || 'USB') + ' · ' + chosenDevice.vendor_id + ':' + chosenDevice.product_id + ' · ' + chosenDevice.path : '';
                var usbOptions = this.devices.map(function (d, i) { return { value: String(i), label: (d.name || 'USB') + ' · ' + d.vendor_id + ':' + d.product_id }; });
                if (this.selectedUSB === 'configured') usbOptions.unshift({ value: 'configured', label: this.cfg.vendor_id + ':' + this.cfg.product_id + (this.cfg.serial ? ' · ' + this.cfg.serial : '') + ' — нет однозначного совпадения в USB' });
                body.push(heading('Подключение ИБП'));
                body.push(h('v-form', { props: { direction: 'horizontal' } }, [
                    formItem('USB-устройство', h('v-single-select', { props: { options: usbOptions, width: '100%', disabled: this.busy, editable: false, clearable: false, placeholder: 'Выберите USB-устройство' }, model: { value: this.selectedUSB, callback: this.selectUSB } }), usbDescription),
                    formItem('', actions([button('Обновить список', this.refreshUSB, false, this.busy), button('Подобрать драйвер', this.detect, false, this.busy || this.cfg.enabled)])),
                    select('driver', 'Драйвер', options(this.drivers)), select('subdriver', 'Субдрайвер', options([''].concat(this.subdrivers))),
                    input('vendor_id', 'Vendor ID'), input('product_id', 'Product ID'), input('serial', 'Серийный номер'),
                    input('poll_interval', 'Интервал опроса, сек.', true), input('listen', 'Адрес сервера NUT'), input('port', 'Порт NUT', true),
                    formItem('', h('v-checkbox', { props: { disabled: this.busy }, model: { value: this.cfg.enabled, callback: function (v) { self.$set(self.cfg, 'enabled', v); } } }, ['Включить мониторинг']))
                ]));
                body.push(note('Для подключения DSM укажите LAN-IP NAS. Адрес 127.0.0.1, скорее всего, не будет принят DSM.' + (this.suggestedListen ? ' Обнаруженный адрес: ' + this.suggestedListen + '.' : ' Автоматически определить один адрес не удалось; укажите его вручную.')));
                body.push(note('Для клиента DSM: ИБП ups, пользователь monuser, пароль secret. Адрес NUT должен совпадать с адресом сервера в настройках ИБП DSM.'));
                body.push(note('Подбор драйвера доступен при выключенном мониторинге. При ошибке применения приложение восстанавливает предыдущие настройки.'));
            } else {
                body.push(heading('Диагностика'), actions([button('Обновить журнал', this.refreshLogs, false, this.busy), button('Скачать диагностику', this.download, false, this.busy), button('Проверить уведомление DSM', this.testNotification, false, this.busy)]));
                body.push(h('pre', { style: { font: '12px/1.6 monospace', whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', marginTop: '16px' } }, [this.logText]));
            }
            var nav = [['status', 'Обзор', 'M3 4h18v16H3z M3 9h18 M7 13h4 M7 16h7'], ['settings', 'Настройки', 'M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8 M12 2v3 M12 19v3 M2 12h3 M19 12h3 M5 5l2 2 M17 17l2 2 M5 19l2-2 M17 7l2-2'], ['logs', 'Журнал', 'M8 5h13 M8 12h13 M8 19h13 M3 5h1 M3 12h1 M3 19h1']];
            var content = h('div', { class: 'ups-native-window', style: { position: 'relative', height: '100%', width: '100%', minHeight: '0', minWidth: '0', overflow: 'hidden', display: 'flex', flexDirection: 'column' } }, [
                h('style', ['.ups-native-window{font-size:14px;color:#414b55;background:#fff}.ups-native-window .ups-section-title{font-size:16px;font-weight:500;color:#2783d1;margin:0 0 20px;padding:0 0 10px;border-bottom:1px dashed #dce0e5}.ups-native-window .ups-nav-button{width:100%;height:42px!important;text-align:left!important;border:0!important;border-radius:5px!important;margin:0 0 0!important;padding:0 14px!important;background:transparent!important;color:#414b55!important;box-shadow:none!important}.ups-native-window .ups-nav-button:hover{background:#f4f7fa!important}.ups-native-window .ups-nav-button.is-current{background:#eaf2fe!important}.ups-native-window .ups-nav-label{display:flex;align-items:center;gap:10px;font-size:15px}.ups-native-window .ups-nav-label svg{color:#667179;flex:none}.ups-native-window .ups-content{overflow-wrap:anywhere}.ups-native-window .ups-content>*{max-width:100%;box-sizing:border-box}.ups-native-window .ups-footbar{min-height:52px;box-sizing:border-box;gap:8px}.ups-native-window .v-form-item{width:100%;min-width:0}.ups-native-window .v-form-item-input{flex:1 1 auto!important;min-width:0!important;width:0!important;max-width:none!important}.ups-native-window .v-form-item-control{width:100%;min-width:0}.ups-native-window .v-form-item-control>div{width:100%;min-width:0}.ups-native-window .ups-content pre{white-space:pre-wrap;word-break:break-word}']),
                h('div', { style: { flex: '1 1 auto', minHeight: '0', minWidth: '0', overflow: 'hidden', display: 'flex' } }, [
                    h('nav', { attrs: { 'aria-label': 'Разделы USB UPS Server' }, style: { flex: '0 0 220px', padding: '12px', borderRight: '1px solid #dce0e5', display: 'flex', flexDirection: 'column', gap: '0' } }, nav.map(function (item) {
                        return h('v-button', { class: ['ups-nav-button', { 'is-current': self.tab === item[0] }], props: { type: 'styleless', disabled: !self.loaded || self.busy }, attrs: { 'aria-current': self.tab === item[0] ? 'page' : null }, on: { click: function () { self.navigate(item[0]); } } }, [h('span', { class: 'ups-nav-label' }, [h('svg', { attrs: { width: 20, height: 20, viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': 2, 'aria-hidden': 'true' } }, [h('path', { attrs: { d: item[2] } })]), item[1]])]);
                    })),
                    h('section', { key: this.tab, class: 'ups-content', style: { flex: '1 1 auto', minWidth: '0', minHeight: '0', overflowX: 'hidden', overflowY: 'auto', padding: '20px 16px', boxSizing: 'border-box' } },
                        (this.loaded && (this.error || this.message) ? [note(this.error || this.message)] : []).concat(body))
                ]),
                this.permissionHelp ? h('div', { style: { position: 'absolute', inset: '0', zIndex: 10, background: 'rgba(0,0,0,.22)', display: 'flex', alignItems: 'center', justifyContent: 'center', padding: '24px', boxSizing: 'border-box' }, on: { keydown: function (event) { if (event.key === 'Escape') self.permissionHelp = null; if (event.key === 'Tab') { event.preventDefault(); var buttons = event.currentTarget.querySelectorAll('button'); var index = Array.prototype.indexOf.call(buttons, document.activeElement); buttons[(index + (event.shiftKey ? buttons.length - 1 : 1)) % buttons.length].focus(); } } } }, [h('section', { attrs: { role: 'alertdialog', 'aria-modal': 'true', 'aria-labelledby': 'ups-usb-access-title' }, style: { width: '620px', maxWidth: '100%', maxHeight: '100%', overflowY: 'auto', overflowX: 'hidden', background: '#fff', borderRadius: '6px', padding: '24px', boxSizing: 'border-box', boxShadow: '0 4px 24px rgba(0,0,0,.25)' }, hook: { insert: function (vnode) { vnode.elm.querySelector('button').focus(); } } }, [h('h2', { attrs: { id: 'ups-usb-access-title' } }, ['Нужен доступ к USB']), note(this.permissionHelp.error), note(this.permissionHelp.hint), h('pre', { style: { whiteSpace: 'pre-wrap', overflowWrap: 'anywhere', background: '#f4f7fa', padding: '12px', userSelect: 'text' } }, [this.permissionHelp.command]), actions([button('Проверить снова', this.refreshUSB, true, this.busy), button('Закрыть', function () { self.permissionHelp = null; }, false, false)])])]) : null,
                this.loaded && this.tab === 'settings' ? h('div', { class: 'ups-footbar', style: { flex: '0 0 auto', borderTop: '1px solid #dce0e5', padding: '12px 20px', display: 'flex', justifyContent: 'flex-end', alignItems: 'center' } }, [
                    button('Отмена', function () { self.initialize(); }, false, this.busy), button('Применить', this.apply, true, this.busy)
                ]) : null
            ]);
            return h('v-app-instance', { props: { className: 'org.usbupsserver.App' } }, [h('v-app-window', {
                ref: 'appWindow', props: { width: 1060, height: 760, minWidth: 900, minHeight: 580, resizable: true }, attrs: { 'syno-id': 'org.usbupsserver.Window' }
            }, [content])]);
        }
    });
}());
