from __future__ import annotations

import tempfile
import unittest
from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parent))

import remote_deploy


class LoadRemoteConfigTests(unittest.TestCase):
    def test_load_remote_config_defaults_to_compose_upstream_mode(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            env_path = Path(tmpdir) / ".remote"
            env_path.write_text(
                "\n".join(
                    [
                        "SSH_HOST=192.168.0.10",
                        "SSH_USER=root",
                        'SSH_PASSWORD="secret"',
                        "REMOTE_DIR=/opt/sub2api",
                        "COMPOSE_FILE=docker-compose.local.yml",
                    ]
                ),
                encoding="utf-8",
            )

            config = remote_deploy.load_remote_config(env_path)

            self.assertEqual(config.image_source, "compose")
            self.assertEqual(config.local_image, "")
            self.assertEqual(config.local_image_tar, "")
            self.assertFalse(config.build_local_image)
            self.assertEqual(config.local_image_build_args, ())

    def test_load_remote_config_reads_local_image_mode_fields(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            env_path = Path(tmpdir) / ".remote"
            env_path.write_text(
                "\n".join(
                    [
                        "SSH_HOST=192.168.0.10",
                        "SSH_USER=root",
                        'SSH_PASSWORD="secret"',
                        "REMOTE_DIR=/opt/sub2api",
                        "COMPOSE_FILE=docker-compose.local.yml",
                        "IMAGE_SOURCE=local_image",
                        "LOCAL_IMAGE=weishaw/sub2api:latest",
                        "LOCAL_IMAGE_TAR=deploy/weishaw-sub2api-latest.tar",
                        "BUILD_LOCAL_IMAGE=true",
                        "LOCAL_IMAGE_BUILD_ARG_NODE_IMAGE=docker.m.daocloud.io/library/node:24-alpine",
                    ]
                ),
                encoding="utf-8",
            )

            config = remote_deploy.load_remote_config(env_path)

            self.assertEqual(config.image_source, "local_image")
            self.assertEqual(config.local_image, "weishaw/sub2api:latest")
            self.assertEqual(config.local_image_tar, "deploy/weishaw-sub2api-latest.tar")
            self.assertTrue(config.build_local_image)
            self.assertEqual(
                config.local_image_build_args,
                ("NODE_IMAGE=docker.m.daocloud.io/library/node:24-alpine",),
            )

    def test_load_remote_config_rejects_local_image_mode_without_image_name(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            env_path = Path(tmpdir) / ".remote"
            env_path.write_text(
                "\n".join(
                    [
                        "SSH_HOST=192.168.0.10",
                        "SSH_USER=root",
                        'SSH_PASSWORD="secret"',
                        "REMOTE_DIR=/opt/sub2api",
                        "COMPOSE_FILE=docker-compose.local.yml",
                        "IMAGE_SOURCE=local_image",
                        "LOCAL_IMAGE_TAR=deploy/weishaw-sub2api-latest.tar",
                    ]
                ),
                encoding="utf-8",
            )

            with self.assertRaisesRegex(ValueError, "LOCAL_IMAGE"):
                remote_deploy.load_remote_config(env_path)


class HelperTests(unittest.TestCase):
    def test_should_upload_path_skips_runtime_data_directories(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            project_root = Path(tmpdir)
            deploy_dir = project_root / "deploy"
            postgres_file = deploy_dir / "postgres_data" / "PG_VERSION"
            runtime_file = deploy_dir / "data" / "config.yaml"
            regular_file = deploy_dir / "docker-compose.local.yml"

            postgres_file.parent.mkdir(parents=True)
            runtime_file.parent.mkdir(parents=True)
            regular_file.parent.mkdir(parents=True, exist_ok=True)
            postgres_file.write_text("18\n", encoding="utf-8")
            runtime_file.write_text("demo\n", encoding="utf-8")
            regular_file.write_text("services:\n", encoding="utf-8")

            self.assertFalse(remote_deploy.should_upload_path(postgres_file, project_root))
            self.assertFalse(remote_deploy.should_upload_path(runtime_file, project_root))
            self.assertTrue(remote_deploy.should_upload_path(regular_file, project_root))

    def test_resolve_local_image_tar_path_uses_project_root_for_relative_paths(self) -> None:
        config = remote_deploy.RemoteDeployConfig(
            ssh_host="127.0.0.1",
            ssh_user="root",
            ssh_password="secret",
            ssh_key_path="",
            ssh_port=22,
            remote_dir="/opt/sub2api",
            compose_file="docker-compose.local.yml",
            image_source="local_image",
            local_image="weishaw/sub2api:latest",
            local_image_tar="deploy/weishaw-sub2api-latest.tar",
            build_local_image=False,
            local_image_build_args=(),
        )

        resolved = remote_deploy.resolve_local_image_tar_path(config, Path("/repo"))

        self.assertEqual(resolved, Path("/repo/deploy/weishaw-sub2api-latest.tar"))

    def test_build_compose_up_command_force_recreates_sub2api_for_local_image_mode(self) -> None:
        command = remote_deploy.build_compose_up_command(
            remote_dir="/opt/sub2api",
            compose_file="docker-compose.local.yml",
            image_source="local_image",
            reuse_existing_data_services=False,
        )

        self.assertIn("up -d --no-deps --force-recreate sub2api", command)
        self.assertNotIn(" up -d\"", command)

    def test_build_compose_up_command_keeps_normal_up_for_compose_mode(self) -> None:
        command = remote_deploy.build_compose_up_command(
            remote_dir="/opt/sub2api",
            compose_file="docker-compose.local.yml",
            image_source="compose",
            reuse_existing_data_services=False,
        )

        self.assertIn("up -d", command)
        self.assertNotIn("--force-recreate", command)

    def test_build_compose_up_command_reuses_existing_data_services(self) -> None:
        command = remote_deploy.build_compose_up_command(
            remote_dir="/opt/sub2api",
            compose_file="docker-compose.local.yml",
            image_source="compose",
            reuse_existing_data_services=True,
        )

        self.assertIn("up -d --no-deps --force-recreate sub2api", command)

    def test_build_docker_build_command_includes_build_args(self) -> None:
        config = remote_deploy.RemoteDeployConfig(
            ssh_host="127.0.0.1",
            ssh_user="root",
            ssh_password="secret",
            ssh_key_path="",
            ssh_port=22,
            remote_dir="/opt/sub2api",
            compose_file="docker-compose.local.yml",
            image_source="local_image",
            local_image="weishaw/sub2api:latest",
            local_image_tar="deploy/weishaw-sub2api-latest.tar",
            build_local_image=True,
            local_image_build_args=("NODE_IMAGE=docker.m.daocloud.io/library/node:24-alpine",),
        )

        command = remote_deploy.build_docker_build_command(config, Path("/repo"))

        self.assertEqual(command[0:3], ["docker", "build", "-t"])
        self.assertIn("weishaw/sub2api:latest", command)
        self.assertIn("--build-arg", command)
        self.assertIn("NODE_IMAGE=docker.m.daocloud.io/library/node:24-alpine", command)
        self.assertEqual(Path(command[-1]), Path("/repo"))

    def test_remote_container_exists_returns_true_for_existing_container(self) -> None:
        class FakeClient:
            def exec_command(self, command: str, timeout: int = 120):
                self.command = command

                class FakeChannel:
                    def recv_exit_status(self) -> int:
                        return 0

                class FakeStream:
                    channel = FakeChannel()

                    def __init__(self, payload: bytes) -> None:
                        self.payload = payload

                    def read(self) -> bytes:
                        return self.payload

                return None, FakeStream(b"exists\n"), FakeStream(b"")

        client = FakeClient()

        self.assertTrue(remote_deploy.remote_container_exists(client, "sub2api-postgres"))
        self.assertIn("docker container inspect sub2api-postgres", client.command)


if __name__ == "__main__":
    unittest.main()
