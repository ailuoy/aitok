"""用模拟 Docker/Git 验证部署控制流程，不构建镜像或操作运行中的服务。"""

import base64
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
MOCK_TOOL = r'''#!/usr/bin/env python3
import json, os, sys
from pathlib import Path
args = sys.argv[1:]
tool = Path(sys.argv[0]).name
scenario = json.loads(os.environ['DEPLOY_TEST_SCENARIO'])
with open(os.environ['DEPLOY_TEST_CALLS'], 'a') as f:
    f.write(json.dumps({'tool': tool, 'args': args, 'build_cpu': os.getenv('BUILD_CPU_COUNT')}) + '\n')
if tool == 'git':
    sys.exit(1 if args[0] == 'diff' and scenario.get('dirty') else 0)
state_path = Path(os.environ['DEPLOY_TEST_STATE'])
state = json.loads(state_path.read_text()) if state_path.exists() else {
    'exists': scenario.get('exists', False),
    'limits': scenario.get('limits', '100000 900000 0')
}
def save(): state_path.write_text(json.dumps(state))
if args[0] == 'info':
    print(scenario.get('cpus', 8)); sys.exit(0)
if args[:2] == ['buildx', 'version'] or args[:2] == ['compose', 'version']:
    sys.exit(0)
if args[:2] == ['buildx', 'inspect']:
    if not state['exists']: sys.exit(1)
    if '--bootstrap' in args and scenario.get('bootstrap_fail'): sys.exit(1)
    print('Name: ' + args[2])
    print('Driver: ' + scenario.get('driver', 'docker-container'))
    print('Nodes:')
    for n in range(scenario.get('nodes', 1)):
        print('Name: ' + args[2] + str(n))
        print('Endpoint: default')
    sys.exit(0)
if args[:2] == ['buildx', 'create']:
    state['exists'] = True
    quota = next(a.split('=', 1)[1] for a in args if a.startswith('cpu-quota='))
    state['limits'] = '100000 ' + quota + ' 0'
    save(); sys.exit(0)
if args[0] == 'inspect':
    print(state['limits']); sys.exit(0)
if args[0] == 'update':
    if scenario.get('update_fail'): sys.exit(1)
    if not scenario.get('ignore_update'):
        state['limits'] = args[args.index('--cpu-period')+1] + ' ' + args[args.index('--cpu-quota')+1] + ' 0'
    save(); sys.exit(0)
if args[0] == 'compose':
    if 'config' in args and scenario.get('config_fail'): sys.exit(1)
    if 'build' in args and scenario.get('build_fail'): sys.exit(1)
    if 'up' in args and scenario.get('up_fail'): sys.exit(1)
    sys.exit(0)
print('未预期的模拟命令', args, file=sys.stderr)
sys.exit(99)
'''


class DeployTest(unittest.TestCase):
    def run_deploy(self, scenario=None, config=None, args=('--skip-pull',)):
        with tempfile.TemporaryDirectory(prefix='aitok-deploy-test-') as directory:
            root = Path(directory)
            for path in ['scripts/deploy.sh', 'scripts/load-env.sh', 'scripts/buildkitd.toml', 'docker-compose.deploy.yml']:
                target = root / path
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(ROOT / path, target)
            values = {
                'DATABASE_URL': 'postgres://test:test@example.invalid/test',
                'APP_BASE_URL': 'https://example.invalid',
                'ADMIN_PASSWORD': 'test-password',
                'JWT_SECRET': 'test-signing-secret-at-least-32-characters',
                'SESSION_ENCRYPTION_KEY': base64.b64encode(b'k' * 32).decode(),
            }
            values.update(config or {})
            (root / '.env').write_text(''.join(f'{key}={value}\n' for key, value in values.items()))
            tools = root / 'bin'
            tools.mkdir()
            for name in ['docker', 'git']:
                path = tools / name
                path.write_text(MOCK_TOOL)
                path.chmod(0o700)
            env = os.environ.copy()
            for key in list(env):
                if key.startswith(('BUILD_', 'DEPLOY_', 'COMPOSE_')):
                    env.pop(key)
            env.update(
                PATH=str(tools) + os.pathsep + env.get('PATH', ''),
                DEPLOY_TEST_SCENARIO=json.dumps(scenario or {}),
                DEPLOY_TEST_CALLS=str(root / 'calls.jsonl'),
                DEPLOY_TEST_STATE=str(root / 'state.json'),
            )
            result = subprocess.run(['bash', str(root / 'scripts/deploy.sh'), *args], env=env, capture_output=True, text=True)
            calls = [json.loads(line) for line in (root / 'calls.jsonl').read_text().splitlines()]
            return result, calls

    def commands(self, calls, action):
        return [c for c in calls if c['tool'] == 'docker' and c['args'][0] == 'compose' and action in c['args']]

    def test_cpu_budget(self):
        for cpus, requested, quota, workers in [(1, '', 50000, 1), (3, '', 150000, 1), (4, '', 200000, 2), (8, '', 400000, 4), (8, '20', 400000, 4), (8, '2', 200000, 2)]:
            with self.subTest(cpus=cpus, requested=requested):
                result, calls = self.run_deploy({'cpus': cpus}, {'BUILD_CPU_COUNT': requested})
                self.assertEqual(result.returncode, 0, result.stderr)
                create = next(c['args'] for c in calls if c['args'][:2] == ['buildx', 'create'])
                self.assertIn(f'cpu-quota={quota}', create)
                self.assertEqual(self.commands(calls, 'build')[0]['build_cpu'], str(workers))
                self.assertEqual(self.commands(calls, 'up')[0]['args'][-2:], ['backend', 'frontend'])
                self.assertIn('--no-build', self.commands(calls, 'up')[0]['args'])
                self.assertLess(calls.index(self.commands(calls, 'build')[0]), calls.index(self.commands(calls, 'up')[0]))

    def test_existing_builder_limits_are_corrected_and_verified(self):
        result, calls = self.run_deploy({'cpus': 4, 'exists': True})
        self.assertEqual(result.returncode, 0, result.stderr)
        update = next(c for c in calls if c['args'][0] == 'update')
        self.assertIn('200000', update['args'])
        self.assertLess(calls.index(update), calls.index(self.commands(calls, 'build')[0]))
        result, calls = self.run_deploy({'cpus': 4, 'exists': True, 'limits': '100000 200000 0'})
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(any(c['args'][0] == 'update' for c in calls))

    def test_unsafe_or_unusable_builder_never_builds(self):
        for scenario in [
            {'exists': True, 'driver': 'docker'},
            {'exists': True, 'nodes': 2},
            {'exists': True, 'ignore_update': True},
            {'exists': True, 'update_fail': True},
            {'bootstrap_fail': True},
            {'cpus': 'invalid'},
            {'config_fail': True},
        ]:
            with self.subTest(scenario=scenario):
                result, calls = self.run_deploy(scenario)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(self.commands(calls, 'build'))
                self.assertFalse(self.commands(calls, 'up'))

    def test_invalid_configuration_never_builds(self):
        for config in [
            {'BUILD_CPU_COUNT': '0'}, {'BUILD_CPU_COUNT': '08'},
            {'COMPOSE_PARALLEL_LIMIT': '-1'}, {'DEPLOY_WAIT_TIMEOUT': 'bad'},
            {'JWT_SECRET': 'short'}, {'SESSION_ENCRYPTION_KEY': 'invalid'},
            {'DEPLOY_BUILDER_NAME': 'bad/name'},
        ]:
            with self.subTest(config=config):
                result, calls = self.run_deploy(config=config)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(self.commands(calls, 'build'))
                self.assertFalse(self.commands(calls, 'up'))

    def test_check_only_has_no_mutations(self):
        result, calls = self.run_deploy({'cpus': 1}, args=('--check',))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('构建上限: 0.5 核', result.stdout)
        for call in calls:
            self.assertEqual(call['tool'], 'docker')
            self.assertIn(call['args'][0], ['compose', 'info', 'buildx'])
            if call['args'][0] == 'compose':
                self.assertTrue('config' in call['args'] or 'version' in call['args'])
            if call['args'][0] == 'buildx':
                self.assertEqual(call['args'][1], 'version')

    def test_build_failure_does_not_replace_services(self):
        result, calls = self.run_deploy({'build_fail': True})
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.commands(calls, 'up'))

    def test_unhealthy_services_are_not_reported_as_success(self):
        result, calls = self.run_deploy({'up_fail': True})
        self.assertNotEqual(result.returncode, 0)
        self.assertTrue(self.commands(calls, 'ps'))
        self.assertNotIn('部署完成', result.stdout)

    def test_pull_reexecutes_once_and_dirty_checkout_stops(self):
        result, calls = self.run_deploy(args=())
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(sum(c['tool'] == 'git' and c['args'][0] == 'pull' for c in calls), 1)
        self.assertEqual(sum(c['args'] == ['compose', 'version'] for c in calls), 2)
        result, calls = self.run_deploy({'dirty': True}, args=())
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(any(c['tool'] == 'git' and c['args'][0] == 'pull' for c in calls))
        self.assertFalse(self.commands(calls, 'build'))


if __name__ == '__main__':
    unittest.main()
