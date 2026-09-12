#!/usr/bin/env bash

# 按单行 KEY=VALUE 读取配置，不将值作为 Shell 代码执行。
load_env_file() {
  local aitok_env_path="$1" aitok_env_line aitok_env_key aitok_env_value
  local aitok_env_number=0
  local aitok_env_assignment='^(export[[:space:]]+)?([A-Za-z_][A-Za-z0-9_]*)[[:space:]]*=(.*)$'
  local aitok_env_single="^'([^']*)'[[:space:]]*(#.*)?$"
  local aitok_env_double='^"([^"]*)"[[:space:]]*(#.*)?$'
  [ -f "$aitok_env_path" ] || return 0

  while IFS= read -r aitok_env_line || [ -n "$aitok_env_line" ]; do
    aitok_env_number=$((aitok_env_number + 1))
    aitok_env_line="${aitok_env_line#"${aitok_env_line%%[![:space:]]*}"}"
    [[ -z "$aitok_env_line" || "$aitok_env_line" == \#* ]] && continue
    if [[ ! "$aitok_env_line" =~ $aitok_env_assignment ]]; then
      echo "环境配置格式错误: ${aitok_env_path}:${aitok_env_number}，应为 KEY=VALUE" >&2
      return 1
    fi
    aitok_env_key="${BASH_REMATCH[2]}"
    aitok_env_value="${BASH_REMATCH[3]}"
    aitok_env_value="${aitok_env_value#"${aitok_env_value%%[![:space:]]*}"}"
    aitok_env_value="${aitok_env_value%"${aitok_env_value##*[![:space:]]}"}"
    case "$aitok_env_value" in
      \#*) aitok_env_value='' ;;
      \"*|\'*)
        if [[ "$aitok_env_value" =~ $aitok_env_single || "$aitok_env_value" =~ $aitok_env_double ]]; then
          aitok_env_value="${BASH_REMATCH[1]}"
        else
          echo "环境配置引号不匹配: ${aitok_env_path}:${aitok_env_number}" >&2
          return 1
        fi
        ;;
      *)
        # 未加引号时，空白后面的 # 表示行尾注释，URL 中的 # 保持原样。
        aitok_env_value="${aitok_env_value%%[[:space:]]#*}"
        aitok_env_value="${aitok_env_value%"${aitok_env_value##*[![:space:]]}"}"
        ;;
    esac
    export "$aitok_env_key=$aitok_env_value"
  done < "$aitok_env_path"
}
