# bash completion for molstar                              -*- shell-script -*-

__molstar_debug()
{
    if [[ -n ${BASH_COMP_DEBUG_FILE:-} ]]; then
        echo "$*" >> "${BASH_COMP_DEBUG_FILE}"
    fi
}

# Homebrew on Macs have version 1.3 of bash-completion which doesn't include
# _init_completion. This is a very minimal version of that function.
__molstar_init_completion()
{
    COMPREPLY=()
    _get_comp_words_by_ref "$@" cur prev words cword
}

__molstar_index_of_word()
{
    local w word=$1
    shift
    index=0
    for w in "$@"; do
        [[ $w = "$word" ]] && return
        index=$((index+1))
    done
    index=-1
}

__molstar_contains_word()
{
    local w word=$1; shift
    for w in "$@"; do
        [[ $w = "$word" ]] && return
    done
    return 1
}

__molstar_handle_go_custom_completion()
{
    __molstar_debug "${FUNCNAME[0]}: cur is ${cur}, words[*] is ${words[*]}, #words[@] is ${#words[@]}"

    local shellCompDirectiveError=1
    local shellCompDirectiveNoSpace=2
    local shellCompDirectiveNoFileComp=4
    local shellCompDirectiveFilterFileExt=8
    local shellCompDirectiveFilterDirs=16

    local out requestComp lastParam lastChar comp directive args

    # Prepare the command to request completions for the program.
    # Calling ${words[0]} instead of directly molstar allows handling aliases
    args=("${words[@]:1}")
    # Disable ActiveHelp which is not supported for bash completion v1
    requestComp="MOLSTAR_ACTIVE_HELP=0 ${words[0]} __completeNoDesc ${args[*]}"

    lastParam=${words[$((${#words[@]}-1))]}
    lastChar=${lastParam:$((${#lastParam}-1)):1}
    __molstar_debug "${FUNCNAME[0]}: lastParam ${lastParam}, lastChar ${lastChar}"

    if [ -z "${cur}" ] && [ "${lastChar}" != "=" ]; then
        # If the last parameter is complete (there is a space following it)
        # We add an extra empty parameter so we can indicate this to the go method.
        __molstar_debug "${FUNCNAME[0]}: Adding extra empty parameter"
        requestComp="${requestComp} \"\""
    fi

    __molstar_debug "${FUNCNAME[0]}: calling ${requestComp}"
    # Use eval to handle any environment variables and such
    out=$(eval "${requestComp}" 2>/dev/null)

    # Extract the directive integer at the very end of the output following a colon (:)
    directive=${out##*:}
    # Remove the directive
    out=${out%:*}
    if [ "${directive}" = "${out}" ]; then
        # There is not directive specified
        directive=0
    fi
    __molstar_debug "${FUNCNAME[0]}: the completion directive is: ${directive}"
    __molstar_debug "${FUNCNAME[0]}: the completions are: ${out}"

    if [ $((directive & shellCompDirectiveError)) -ne 0 ]; then
        # Error code.  No completion.
        __molstar_debug "${FUNCNAME[0]}: received error from custom completion go code"
        return
    else
        if [ $((directive & shellCompDirectiveNoSpace)) -ne 0 ]; then
            if [[ $(type -t compopt) = "builtin" ]]; then
                __molstar_debug "${FUNCNAME[0]}: activating no space"
                compopt -o nospace
            fi
        fi
        if [ $((directive & shellCompDirectiveNoFileComp)) -ne 0 ]; then
            if [[ $(type -t compopt) = "builtin" ]]; then
                __molstar_debug "${FUNCNAME[0]}: activating no file completion"
                compopt +o default
            fi
        fi
    fi

    if [ $((directive & shellCompDirectiveFilterFileExt)) -ne 0 ]; then
        # File extension filtering
        local fullFilter filter filteringCmd
        # Do not use quotes around the $out variable or else newline
        # characters will be kept.
        for filter in ${out}; do
            fullFilter+="$filter|"
        done

        filteringCmd="_filedir $fullFilter"
        __molstar_debug "File filtering command: $filteringCmd"
        $filteringCmd
    elif [ $((directive & shellCompDirectiveFilterDirs)) -ne 0 ]; then
        # File completion for directories only
        local subdir
        # Use printf to strip any trailing newline
        subdir=$(printf "%s" "${out}")
        if [ -n "$subdir" ]; then
            __molstar_debug "Listing directories in $subdir"
            __molstar_handle_subdirs_in_dir_flag "$subdir"
        else
            __molstar_debug "Listing directories in ."
            _filedir -d
        fi
    else
        while IFS='' read -r comp; do
            COMPREPLY+=("$comp")
        done < <(compgen -W "${out}" -- "$cur")
    fi
}

__molstar_handle_reply()
{
    __molstar_debug "${FUNCNAME[0]}"
    local comp
    case $cur in
        -*)
            if [[ $(type -t compopt) = "builtin" ]]; then
                compopt -o nospace
            fi
            local allflags
            if [ ${#must_have_one_flag[@]} -ne 0 ]; then
                allflags=("${must_have_one_flag[@]}")
            else
                allflags=("${flags[*]} ${two_word_flags[*]}")
            fi
            while IFS='' read -r comp; do
                COMPREPLY+=("$comp")
            done < <(compgen -W "${allflags[*]}" -- "$cur")
            if [[ $(type -t compopt) = "builtin" ]]; then
                [[ "${COMPREPLY[0]}" == *= ]] || compopt +o nospace
            fi

            # complete after --flag=abc
            if [[ $cur == *=* ]]; then
                if [[ $(type -t compopt) = "builtin" ]]; then
                    compopt +o nospace
                fi

                local index flag
                flag="${cur%=*}"
                __molstar_index_of_word "${flag}" "${flags_with_completion[@]}"
                COMPREPLY=()
                if [[ ${index} -ge 0 ]]; then
                    PREFIX=""
                    cur="${cur#*=}"
                    ${flags_completion[${index}]}
                    if [ -n "${ZSH_VERSION:-}" ]; then
                        # zsh completion needs --flag= prefix
                        eval "COMPREPLY=( \"\${COMPREPLY[@]/#/${flag}=}\" )"
                    fi
                fi
            fi

            if [[ -z "${flag_parsing_disabled}" ]]; then
                # If flag parsing is enabled, we have completed the flags and can return.
                # If flag parsing is disabled, we may not know all (or any) of the flags, so we fallthrough
                # to possibly call handle_go_custom_completion.
                return 0;
            fi
            ;;
    esac

    # check if we are handling a flag with special work handling
    local index
    __molstar_index_of_word "${prev}" "${flags_with_completion[@]}"
    if [[ ${index} -ge 0 ]]; then
        ${flags_completion[${index}]}
        return
    fi

    # we are parsing a flag and don't have a special handler, no completion
    if [[ ${cur} != "${words[cword]}" ]]; then
        return
    fi

    local completions
    completions=("${commands[@]}")
    if [[ ${#must_have_one_noun[@]} -ne 0 ]]; then
        completions+=("${must_have_one_noun[@]}")
    elif [[ -n "${has_completion_function}" ]]; then
        # if a go completion function is provided, defer to that function
        __molstar_handle_go_custom_completion
    fi
    if [[ ${#must_have_one_flag[@]} -ne 0 ]]; then
        completions+=("${must_have_one_flag[@]}")
    fi
    while IFS='' read -r comp; do
        COMPREPLY+=("$comp")
    done < <(compgen -W "${completions[*]}" -- "$cur")

    if [[ ${#COMPREPLY[@]} -eq 0 && ${#noun_aliases[@]} -gt 0 && ${#must_have_one_noun[@]} -ne 0 ]]; then
        while IFS='' read -r comp; do
            COMPREPLY+=("$comp")
        done < <(compgen -W "${noun_aliases[*]}" -- "$cur")
    fi

    if [[ ${#COMPREPLY[@]} -eq 0 ]]; then
        if declare -F __molstar_custom_func >/dev/null; then
            # try command name qualified custom func
            __molstar_custom_func
        else
            # otherwise fall back to unqualified for compatibility
            declare -F __custom_func >/dev/null && __custom_func
        fi
    fi

    # available in bash-completion >= 2, not always present on macOS
    if declare -F __ltrim_colon_completions >/dev/null; then
        __ltrim_colon_completions "$cur"
    fi

    # If there is only 1 completion and it is a flag with an = it will be completed
    # but we don't want a space after the =
    if [[ "${#COMPREPLY[@]}" -eq "1" ]] && [[ $(type -t compopt) = "builtin" ]] && [[ "${COMPREPLY[0]}" == --*= ]]; then
       compopt -o nospace
    fi
}

# The arguments should be in the form "ext1|ext2|extn"
__molstar_handle_filename_extension_flag()
{
    local ext="$1"
    _filedir "@(${ext})"
}

__molstar_handle_subdirs_in_dir_flag()
{
    local dir="$1"
    pushd "${dir}" >/dev/null 2>&1 && _filedir -d && popd >/dev/null 2>&1 || return
}

__molstar_handle_flag()
{
    __molstar_debug "${FUNCNAME[0]}: c is $c words[c] is ${words[c]}"

    # if a command required a flag, and we found it, unset must_have_one_flag()
    local flagname=${words[c]}
    local flagvalue=""
    # if the word contained an =
    if [[ ${words[c]} == *"="* ]]; then
        flagvalue=${flagname#*=} # take in as flagvalue after the =
        flagname=${flagname%=*} # strip everything after the =
        flagname="${flagname}=" # but put the = back
    fi
    __molstar_debug "${FUNCNAME[0]}: looking for ${flagname}"
    if __molstar_contains_word "${flagname}" "${must_have_one_flag[@]}"; then
        must_have_one_flag=()
    fi

    # if you set a flag which only applies to this command, don't show subcommands
    if __molstar_contains_word "${flagname}" "${local_nonpersistent_flags[@]}"; then
      commands=()
    fi

    # keep flag value with flagname as flaghash
    # flaghash variable is an associative array which is only supported in bash > 3.
    if [[ -z "${BASH_VERSION:-}" || "${BASH_VERSINFO[0]:-}" -gt 3 ]]; then
        if [ -n "${flagvalue}" ] ; then
            flaghash[${flagname}]=${flagvalue}
        elif [ -n "${words[ $((c+1)) ]}" ] ; then
            flaghash[${flagname}]=${words[ $((c+1)) ]}
        else
            flaghash[${flagname}]="true" # pad "true" for bool flag
        fi
    fi

    # skip the argument to a two word flag
    if [[ ${words[c]} != *"="* ]] && __molstar_contains_word "${words[c]}" "${two_word_flags[@]}"; then
        __molstar_debug "${FUNCNAME[0]}: found a flag ${words[c]}, skip the next argument"
        c=$((c+1))
        # if we are looking for a flags value, don't show commands
        if [[ $c -eq $cword ]]; then
            commands=()
        fi
    fi

    c=$((c+1))

}

__molstar_handle_noun()
{
    __molstar_debug "${FUNCNAME[0]}: c is $c words[c] is ${words[c]}"

    if __molstar_contains_word "${words[c]}" "${must_have_one_noun[@]}"; then
        must_have_one_noun=()
    elif __molstar_contains_word "${words[c]}" "${noun_aliases[@]}"; then
        must_have_one_noun=()
    fi

    nouns+=("${words[c]}")
    c=$((c+1))
}

__molstar_handle_command()
{
    __molstar_debug "${FUNCNAME[0]}: c is $c words[c] is ${words[c]}"

    local next_command
    if [[ -n ${last_command} ]]; then
        next_command="_${last_command}_${words[c]//:/__}"
    else
        if [[ $c -eq 0 ]]; then
            next_command="_molstar_root_command"
        else
            next_command="_${words[c]//:/__}"
        fi
    fi
    c=$((c+1))
    __molstar_debug "${FUNCNAME[0]}: looking for ${next_command}"
    declare -F "$next_command" >/dev/null && $next_command
}

__molstar_handle_word()
{
    if [[ $c -ge $cword ]]; then
        __molstar_handle_reply
        return
    fi
    __molstar_debug "${FUNCNAME[0]}: c is $c words[c] is ${words[c]}"
    if [[ "${words[c]}" == -* ]]; then
        __molstar_handle_flag
    elif __molstar_contains_word "${words[c]}" "${commands[@]}"; then
        __molstar_handle_command
    elif [[ $c -eq 0 ]]; then
        __molstar_handle_command
    elif __molstar_contains_word "${words[c]}" "${command_aliases[@]}"; then
        # aliashash variable is an associative array which is only supported in bash > 3.
        if [[ -z "${BASH_VERSION:-}" || "${BASH_VERSINFO[0]:-}" -gt 3 ]]; then
            words[c]=${aliashash[${words[c]}]}
            __molstar_handle_command
        else
            __molstar_handle_noun
        fi
    else
        __molstar_handle_noun
    fi
    __molstar_handle_word
}

_molstar_agent_doctor()
{
    last_command="molstar_agent_doctor"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--deep")
    local_nonpersistent_flags+=("--deep")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--keep")
    local_nonpersistent_flags+=("--keep")
    flags+=("--out-dir=")
    two_word_flags+=("--out-dir")
    local_nonpersistent_flags+=("--out-dir")
    local_nonpersistent_flags+=("--out-dir=")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_agent()
{
    last_command="molstar_agent"

    command_aliases=()

    commands=()
    commands+=("doctor")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()


    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_batch()
{
    last_command="molstar_batch"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--allow-host=")
    two_word_flags+=("--allow-host")
    local_nonpersistent_flags+=("--allow-host")
    local_nonpersistent_flags+=("--allow-host=")
    flags+=("--allow-path=")
    two_word_flags+=("--allow-path")
    local_nonpersistent_flags+=("--allow-path")
    local_nonpersistent_flags+=("--allow-path=")
    flags+=("--cache=")
    two_word_flags+=("--cache")
    local_nonpersistent_flags+=("--cache")
    local_nonpersistent_flags+=("--cache=")
    flags+=("--cache-dir=")
    two_word_flags+=("--cache-dir")
    local_nonpersistent_flags+=("--cache-dir")
    local_nonpersistent_flags+=("--cache-dir=")
    flags+=("--concurrency=")
    two_word_flags+=("--concurrency")
    local_nonpersistent_flags+=("--concurrency")
    local_nonpersistent_flags+=("--concurrency=")
    flags+=("--continue-on-error")
    local_nonpersistent_flags+=("--continue-on-error")
    flags+=("--dry-run")
    local_nonpersistent_flags+=("--dry-run")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--manifest=")
    two_word_flags+=("--manifest")
    local_nonpersistent_flags+=("--manifest")
    local_nonpersistent_flags+=("--manifest=")
    flags+=("--max-atoms=")
    two_word_flags+=("--max-atoms")
    local_nonpersistent_flags+=("--max-atoms")
    local_nonpersistent_flags+=("--max-atoms=")
    flags+=("--max-download-bytes=")
    two_word_flags+=("--max-download-bytes")
    local_nonpersistent_flags+=("--max-download-bytes")
    local_nonpersistent_flags+=("--max-download-bytes=")
    flags+=("--max-outputs=")
    two_word_flags+=("--max-outputs")
    local_nonpersistent_flags+=("--max-outputs")
    local_nonpersistent_flags+=("--max-outputs=")
    flags+=("--max-pixels=")
    two_word_flags+=("--max-pixels")
    local_nonpersistent_flags+=("--max-pixels")
    local_nonpersistent_flags+=("--max-pixels=")
    flags+=("--no-cache")
    local_nonpersistent_flags+=("--no-cache")
    flags+=("--no-network")
    local_nonpersistent_flags+=("--no-network")
    flags+=("--offline")
    local_nonpersistent_flags+=("--offline")
    flags+=("--out=")
    two_word_flags+=("--out")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    flags+=("--out-dir=")
    two_word_flags+=("--out-dir")
    local_nonpersistent_flags+=("--out-dir")
    local_nonpersistent_flags+=("--out-dir=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    local_nonpersistent_flags+=("--profile")
    local_nonpersistent_flags+=("--profile=")
    flags+=("--quiet")
    local_nonpersistent_flags+=("--quiet")
    flags+=("--renderer-command=")
    two_word_flags+=("--renderer-command")
    local_nonpersistent_flags+=("--renderer-command")
    local_nonpersistent_flags+=("--renderer-command=")
    flags+=("--renderer-mode=")
    two_word_flags+=("--renderer-mode")
    local_nonpersistent_flags+=("--renderer-mode")
    local_nonpersistent_flags+=("--renderer-mode=")
    flags+=("--retries=")
    two_word_flags+=("--retries")
    local_nonpersistent_flags+=("--retries")
    local_nonpersistent_flags+=("--retries=")
    flags+=("--skip-existing")
    local_nonpersistent_flags+=("--skip-existing")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout=")
    flags+=("--verbose")
    local_nonpersistent_flags+=("--verbose")
    flags+=("--worker-command=")
    two_word_flags+=("--worker-command")
    local_nonpersistent_flags+=("--worker-command")
    local_nonpersistent_flags+=("--worker-command=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_bench()
{
    last_command="molstar_bench"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--allow-host=")
    two_word_flags+=("--allow-host")
    local_nonpersistent_flags+=("--allow-host")
    local_nonpersistent_flags+=("--allow-host=")
    flags+=("--allow-path=")
    two_word_flags+=("--allow-path")
    local_nonpersistent_flags+=("--allow-path")
    local_nonpersistent_flags+=("--allow-path=")
    flags+=("--assembly=")
    two_word_flags+=("--assembly")
    local_nonpersistent_flags+=("--assembly")
    local_nonpersistent_flags+=("--assembly=")
    flags+=("--background=")
    two_word_flags+=("--background")
    local_nonpersistent_flags+=("--background")
    local_nonpersistent_flags+=("--background=")
    flags+=("--baseline=")
    two_word_flags+=("--baseline")
    local_nonpersistent_flags+=("--baseline")
    local_nonpersistent_flags+=("--baseline=")
    flags+=("--cache=")
    two_word_flags+=("--cache")
    local_nonpersistent_flags+=("--cache")
    local_nonpersistent_flags+=("--cache=")
    flags+=("--cache-dir=")
    two_word_flags+=("--cache-dir")
    local_nonpersistent_flags+=("--cache-dir")
    local_nonpersistent_flags+=("--cache-dir=")
    flags+=("--continue-on-error")
    local_nonpersistent_flags+=("--continue-on-error")
    flags+=("--demo")
    local_nonpersistent_flags+=("--demo")
    flags+=("--dry-run")
    local_nonpersistent_flags+=("--dry-run")
    flags+=("--fail-regression=")
    two_word_flags+=("--fail-regression")
    local_nonpersistent_flags+=("--fail-regression")
    local_nonpersistent_flags+=("--fail-regression=")
    flags+=("--format=")
    two_word_flags+=("--format")
    local_nonpersistent_flags+=("--format")
    local_nonpersistent_flags+=("--format=")
    flags+=("--iterations=")
    two_word_flags+=("--iterations")
    local_nonpersistent_flags+=("--iterations")
    local_nonpersistent_flags+=("--iterations=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--label=")
    two_word_flags+=("--label")
    local_nonpersistent_flags+=("--label")
    local_nonpersistent_flags+=("--label=")
    flags+=("--max-atoms=")
    two_word_flags+=("--max-atoms")
    local_nonpersistent_flags+=("--max-atoms")
    local_nonpersistent_flags+=("--max-atoms=")
    flags+=("--max-download-bytes=")
    two_word_flags+=("--max-download-bytes")
    local_nonpersistent_flags+=("--max-download-bytes")
    local_nonpersistent_flags+=("--max-download-bytes=")
    flags+=("--max-outputs=")
    two_word_flags+=("--max-outputs")
    local_nonpersistent_flags+=("--max-outputs")
    local_nonpersistent_flags+=("--max-outputs=")
    flags+=("--max-pixels=")
    two_word_flags+=("--max-pixels")
    local_nonpersistent_flags+=("--max-pixels")
    local_nonpersistent_flags+=("--max-pixels=")
    flags+=("--max-regression-percent=")
    two_word_flags+=("--max-regression-percent")
    local_nonpersistent_flags+=("--max-regression-percent")
    local_nonpersistent_flags+=("--max-regression-percent=")
    flags+=("--no-cache")
    local_nonpersistent_flags+=("--no-cache")
    flags+=("--no-network")
    local_nonpersistent_flags+=("--no-network")
    flags+=("--offline")
    local_nonpersistent_flags+=("--offline")
    flags+=("--out-dir=")
    two_word_flags+=("--out-dir")
    local_nonpersistent_flags+=("--out-dir")
    local_nonpersistent_flags+=("--out-dir=")
    flags+=("--preset=")
    two_word_flags+=("--preset")
    local_nonpersistent_flags+=("--preset")
    local_nonpersistent_flags+=("--preset=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    local_nonpersistent_flags+=("--profile")
    local_nonpersistent_flags+=("--profile=")
    flags+=("--provider=")
    two_word_flags+=("--provider")
    local_nonpersistent_flags+=("--provider")
    local_nonpersistent_flags+=("--provider=")
    flags+=("--quiet")
    local_nonpersistent_flags+=("--quiet")
    flags+=("--renderer-command=")
    two_word_flags+=("--renderer-command")
    local_nonpersistent_flags+=("--renderer-command")
    local_nonpersistent_flags+=("--renderer-command=")
    flags+=("--renderer-mode=")
    two_word_flags+=("--renderer-mode")
    local_nonpersistent_flags+=("--renderer-mode")
    local_nonpersistent_flags+=("--renderer-mode=")
    flags+=("--report=")
    two_word_flags+=("--report")
    local_nonpersistent_flags+=("--report")
    local_nonpersistent_flags+=("--report=")
    flags+=("--size=")
    two_word_flags+=("--size")
    local_nonpersistent_flags+=("--size")
    local_nonpersistent_flags+=("--size=")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout=")
    flags+=("--verbose")
    local_nonpersistent_flags+=("--verbose")
    flags+=("--warmup=")
    two_word_flags+=("--warmup")
    local_nonpersistent_flags+=("--warmup")
    local_nonpersistent_flags+=("--warmup=")
    flags+=("--worker-command=")
    two_word_flags+=("--worker-command")
    local_nonpersistent_flags+=("--worker-command")
    local_nonpersistent_flags+=("--worker-command=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_cache_explain()
{
    last_command="molstar_cache_explain"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--cache=")
    two_word_flags+=("--cache")
    local_nonpersistent_flags+=("--cache")
    local_nonpersistent_flags+=("--cache=")
    flags+=("--format=")
    two_word_flags+=("--format")
    local_nonpersistent_flags+=("--format")
    local_nonpersistent_flags+=("--format=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--provider=")
    two_word_flags+=("--provider")
    local_nonpersistent_flags+=("--provider")
    local_nonpersistent_flags+=("--provider=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_cache_list()
{
    last_command="molstar_cache_list"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--cache=")
    two_word_flags+=("--cache")
    local_nonpersistent_flags+=("--cache")
    local_nonpersistent_flags+=("--cache=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_cache_prune()
{
    last_command="molstar_cache_prune"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--cache=")
    two_word_flags+=("--cache")
    local_nonpersistent_flags+=("--cache")
    local_nonpersistent_flags+=("--cache=")
    flags+=("--dry-run")
    local_nonpersistent_flags+=("--dry-run")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--older-than=")
    two_word_flags+=("--older-than")
    local_nonpersistent_flags+=("--older-than")
    local_nonpersistent_flags+=("--older-than=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_cache_verify()
{
    last_command="molstar_cache_verify"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--cache=")
    two_word_flags+=("--cache")
    local_nonpersistent_flags+=("--cache")
    local_nonpersistent_flags+=("--cache=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_cache()
{
    last_command="molstar_cache"

    command_aliases=()

    commands=()
    commands+=("explain")
    commands+=("list")
    commands+=("prune")
    commands+=("verify")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()


    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_capabilities()
{
    last_command="molstar_capabilities"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--probe-worker")
    local_nonpersistent_flags+=("--probe-worker")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_compat_check()
{
    last_command="molstar_compat_check"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--out-dir=")
    two_word_flags+=("--out-dir")
    local_nonpersistent_flags+=("--out-dir")
    local_nonpersistent_flags+=("--out-dir=")
    flags+=("--render")
    local_nonpersistent_flags+=("--render")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_compat()
{
    last_command="molstar_compat"

    command_aliases=()

    commands=()
    commands+=("check")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()


    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_completion()
{
    last_command="molstar_completion"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--help")
    flags+=("-h")
    local_nonpersistent_flags+=("--help")
    local_nonpersistent_flags+=("-h")
    flags+=("--out=")
    two_word_flags+=("--out")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    flags+=("--out-dir=")
    two_word_flags+=("--out-dir")
    local_nonpersistent_flags+=("--out-dir")
    local_nonpersistent_flags+=("--out-dir=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_diagnose()
{
    last_command="molstar_diagnose"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--bundle")
    local_nonpersistent_flags+=("--bundle")
    flags+=("--ci-artifact=")
    two_word_flags+=("--ci-artifact")
    local_nonpersistent_flags+=("--ci-artifact")
    local_nonpersistent_flags+=("--ci-artifact=")
    flags+=("--dir=")
    two_word_flags+=("--dir")
    local_nonpersistent_flags+=("--dir")
    local_nonpersistent_flags+=("--dir=")
    flags+=("--include-inputs")
    local_nonpersistent_flags+=("--include-inputs")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--max-input-bytes=")
    two_word_flags+=("--max-input-bytes")
    local_nonpersistent_flags+=("--max-input-bytes")
    local_nonpersistent_flags+=("--max-input-bytes=")
    flags+=("--max-single-input-bytes=")
    two_word_flags+=("--max-single-input-bytes")
    local_nonpersistent_flags+=("--max-single-input-bytes")
    local_nonpersistent_flags+=("--max-single-input-bytes=")
    flags+=("--out=")
    two_word_flags+=("--out")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    local_nonpersistent_flags+=("-o")
    flags+=("--redact-env")
    local_nonpersistent_flags+=("--redact-env")
    flags+=("--redact-inputs")
    local_nonpersistent_flags+=("--redact-inputs")
    flags+=("--redact-paths")
    local_nonpersistent_flags+=("--redact-paths")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_docs()
{
    last_command="molstar_docs"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--out=")
    two_word_flags+=("--out")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_doctor()
{
    last_command="molstar_doctor"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--cache=")
    two_word_flags+=("--cache")
    local_nonpersistent_flags+=("--cache")
    local_nonpersistent_flags+=("--cache=")
    flags+=("--config=")
    two_word_flags+=("--config")
    local_nonpersistent_flags+=("--config")
    local_nonpersistent_flags+=("--config=")
    flags+=("--fix")
    local_nonpersistent_flags+=("--fix")
    flags+=("--home=")
    two_word_flags+=("--home")
    local_nonpersistent_flags+=("--home")
    local_nonpersistent_flags+=("--home=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--probe-size=")
    two_word_flags+=("--probe-size")
    local_nonpersistent_flags+=("--probe-size")
    local_nonpersistent_flags+=("--probe-size=")
    flags+=("--skip-probe")
    local_nonpersistent_flags+=("--skip-probe")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_examples_show()
{
    last_command="molstar_examples_show"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_examples()
{
    last_command="molstar_examples"

    command_aliases=()

    commands=()
    commands+=("show")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_fixtures_list()
{
    last_command="molstar_fixtures_list"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_fixtures_verify()
{
    last_command="molstar_fixtures_verify"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--dry-run")
    local_nonpersistent_flags+=("--dry-run")
    flags+=("--golden")
    local_nonpersistent_flags+=("--golden")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--keep-going")
    local_nonpersistent_flags+=("--keep-going")
    flags+=("--network")
    local_nonpersistent_flags+=("--network")
    flags+=("--out-dir=")
    two_word_flags+=("--out-dir")
    local_nonpersistent_flags+=("--out-dir")
    local_nonpersistent_flags+=("--out-dir=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_fixtures()
{
    last_command="molstar_fixtures"

    command_aliases=()

    commands=()
    commands+=("list")
    commands+=("verify")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()


    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_help()
{
    last_command="molstar_help"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()


    must_have_one_flag=()
    must_have_one_noun=()
    has_completion_function=1
    noun_aliases=()
}

_molstar_inspect()
{
    last_command="molstar_inspect"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--allow-host=")
    two_word_flags+=("--allow-host")
    local_nonpersistent_flags+=("--allow-host")
    local_nonpersistent_flags+=("--allow-host=")
    flags+=("--allow-path=")
    two_word_flags+=("--allow-path")
    local_nonpersistent_flags+=("--allow-path")
    local_nonpersistent_flags+=("--allow-path=")
    flags+=("--cache=")
    two_word_flags+=("--cache")
    local_nonpersistent_flags+=("--cache")
    local_nonpersistent_flags+=("--cache=")
    flags+=("--cache-dir=")
    two_word_flags+=("--cache-dir")
    local_nonpersistent_flags+=("--cache-dir")
    local_nonpersistent_flags+=("--cache-dir=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--max-atoms=")
    two_word_flags+=("--max-atoms")
    local_nonpersistent_flags+=("--max-atoms")
    local_nonpersistent_flags+=("--max-atoms=")
    flags+=("--max-download-bytes=")
    two_word_flags+=("--max-download-bytes")
    local_nonpersistent_flags+=("--max-download-bytes")
    local_nonpersistent_flags+=("--max-download-bytes=")
    flags+=("--max-outputs=")
    two_word_flags+=("--max-outputs")
    local_nonpersistent_flags+=("--max-outputs")
    local_nonpersistent_flags+=("--max-outputs=")
    flags+=("--max-pixels=")
    two_word_flags+=("--max-pixels")
    local_nonpersistent_flags+=("--max-pixels")
    local_nonpersistent_flags+=("--max-pixels=")
    flags+=("--no-cache")
    local_nonpersistent_flags+=("--no-cache")
    flags+=("--no-network")
    local_nonpersistent_flags+=("--no-network")
    flags+=("--no-prepare")
    local_nonpersistent_flags+=("--no-prepare")
    flags+=("--offline")
    local_nonpersistent_flags+=("--offline")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    local_nonpersistent_flags+=("--profile")
    local_nonpersistent_flags+=("--profile=")
    flags+=("--renderer-command=")
    two_word_flags+=("--renderer-command")
    local_nonpersistent_flags+=("--renderer-command")
    local_nonpersistent_flags+=("--renderer-command=")
    flags+=("--select=")
    two_word_flags+=("--select")
    local_nonpersistent_flags+=("--select")
    local_nonpersistent_flags+=("--select=")
    flags+=("--semantic")
    local_nonpersistent_flags+=("--semantic")
    flags+=("--strict-semantic")
    local_nonpersistent_flags+=("--strict-semantic")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_install-artifact()
{
    last_command="molstar_install-artifact"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--artifact=")
    two_word_flags+=("--artifact")
    local_nonpersistent_flags+=("--artifact")
    local_nonpersistent_flags+=("--artifact=")
    flags+=("--bin-dir=")
    two_word_flags+=("--bin-dir")
    local_nonpersistent_flags+=("--bin-dir")
    local_nonpersistent_flags+=("--bin-dir=")
    flags+=("--config=")
    two_word_flags+=("--config")
    local_nonpersistent_flags+=("--config")
    local_nonpersistent_flags+=("--config=")
    flags+=("--force")
    local_nonpersistent_flags+=("--force")
    flags+=("--install-deps")
    local_nonpersistent_flags+=("--install-deps")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--name=")
    two_word_flags+=("--name")
    local_nonpersistent_flags+=("--name")
    local_nonpersistent_flags+=("--name=")
    flags+=("--prefix=")
    two_word_flags+=("--prefix")
    local_nonpersistent_flags+=("--prefix")
    local_nonpersistent_flags+=("--prefix=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_install-local()
{
    last_command="molstar_install-local"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--bin-dir=")
    two_word_flags+=("--bin-dir")
    local_nonpersistent_flags+=("--bin-dir")
    local_nonpersistent_flags+=("--bin-dir=")
    flags+=("--config=")
    two_word_flags+=("--config")
    local_nonpersistent_flags+=("--config")
    local_nonpersistent_flags+=("--config=")
    flags+=("--force")
    local_nonpersistent_flags+=("--force")
    flags+=("--home=")
    two_word_flags+=("--home")
    local_nonpersistent_flags+=("--home")
    local_nonpersistent_flags+=("--home=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--name=")
    two_word_flags+=("--name")
    local_nonpersistent_flags+=("--name")
    local_nonpersistent_flags+=("--name=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_job_examples_list()
{
    last_command="molstar_job_examples_list"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_job_examples_show()
{
    last_command="molstar_job_examples_show"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_job_examples()
{
    last_command="molstar_job_examples"

    command_aliases=()

    commands=()
    commands+=("list")
    commands+=("show")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()


    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_job_explain()
{
    last_command="molstar_job_explain"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--allow-host=")
    two_word_flags+=("--allow-host")
    local_nonpersistent_flags+=("--allow-host")
    local_nonpersistent_flags+=("--allow-host=")
    flags+=("--allow-path=")
    two_word_flags+=("--allow-path")
    local_nonpersistent_flags+=("--allow-path")
    local_nonpersistent_flags+=("--allow-path=")
    flags+=("--assembly=")
    two_word_flags+=("--assembly")
    local_nonpersistent_flags+=("--assembly")
    local_nonpersistent_flags+=("--assembly=")
    flags+=("--background=")
    two_word_flags+=("--background")
    local_nonpersistent_flags+=("--background")
    local_nonpersistent_flags+=("--background=")
    flags+=("--cache=")
    two_word_flags+=("--cache")
    local_nonpersistent_flags+=("--cache")
    local_nonpersistent_flags+=("--cache=")
    flags+=("--cache-dir=")
    two_word_flags+=("--cache-dir")
    local_nonpersistent_flags+=("--cache-dir")
    local_nonpersistent_flags+=("--cache-dir=")
    flags+=("--color=")
    two_word_flags+=("--color")
    local_nonpersistent_flags+=("--color")
    local_nonpersistent_flags+=("--color=")
    flags+=("--focus=")
    two_word_flags+=("--focus")
    local_nonpersistent_flags+=("--focus")
    local_nonpersistent_flags+=("--focus=")
    flags+=("--format-input=")
    two_word_flags+=("--format-input")
    local_nonpersistent_flags+=("--format-input")
    local_nonpersistent_flags+=("--format-input=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--max-atoms=")
    two_word_flags+=("--max-atoms")
    local_nonpersistent_flags+=("--max-atoms")
    local_nonpersistent_flags+=("--max-atoms=")
    flags+=("--max-download-bytes=")
    two_word_flags+=("--max-download-bytes")
    local_nonpersistent_flags+=("--max-download-bytes")
    local_nonpersistent_flags+=("--max-download-bytes=")
    flags+=("--max-outputs=")
    two_word_flags+=("--max-outputs")
    local_nonpersistent_flags+=("--max-outputs")
    local_nonpersistent_flags+=("--max-outputs=")
    flags+=("--max-pixels=")
    two_word_flags+=("--max-pixels")
    local_nonpersistent_flags+=("--max-pixels")
    local_nonpersistent_flags+=("--max-pixels=")
    flags+=("--no-cache")
    local_nonpersistent_flags+=("--no-cache")
    flags+=("--no-network")
    local_nonpersistent_flags+=("--no-network")
    flags+=("--offline")
    local_nonpersistent_flags+=("--offline")
    flags+=("--out=")
    two_word_flags+=("--out")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    local_nonpersistent_flags+=("-o")
    flags+=("--preset=")
    two_word_flags+=("--preset")
    local_nonpersistent_flags+=("--preset")
    local_nonpersistent_flags+=("--preset=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    local_nonpersistent_flags+=("--profile")
    local_nonpersistent_flags+=("--profile=")
    flags+=("--provider=")
    two_word_flags+=("--provider")
    local_nonpersistent_flags+=("--provider")
    local_nonpersistent_flags+=("--provider=")
    flags+=("--repr=")
    two_word_flags+=("--repr")
    local_nonpersistent_flags+=("--repr")
    local_nonpersistent_flags+=("--repr=")
    flags+=("--select=")
    two_word_flags+=("--select")
    local_nonpersistent_flags+=("--select")
    local_nonpersistent_flags+=("--select=")
    flags+=("--size=")
    two_word_flags+=("--size")
    local_nonpersistent_flags+=("--size")
    local_nonpersistent_flags+=("--size=")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout=")
    flags+=("--view=")
    two_word_flags+=("--view")
    local_nonpersistent_flags+=("--view")
    local_nonpersistent_flags+=("--view=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_job_init()
{
    last_command="molstar_job_init"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--format=")
    two_word_flags+=("--format")
    local_nonpersistent_flags+=("--format")
    local_nonpersistent_flags+=("--format=")
    flags+=("--out=")
    two_word_flags+=("--out")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    local_nonpersistent_flags+=("-o")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_job_migrate()
{
    last_command="molstar_job_migrate"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--write=")
    two_word_flags+=("--write")
    local_nonpersistent_flags+=("--write")
    local_nonpersistent_flags+=("--write=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_job_normalize()
{
    last_command="molstar_job_normalize"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--allow-host=")
    two_word_flags+=("--allow-host")
    local_nonpersistent_flags+=("--allow-host")
    local_nonpersistent_flags+=("--allow-host=")
    flags+=("--allow-path=")
    two_word_flags+=("--allow-path")
    local_nonpersistent_flags+=("--allow-path")
    local_nonpersistent_flags+=("--allow-path=")
    flags+=("--assembly=")
    two_word_flags+=("--assembly")
    local_nonpersistent_flags+=("--assembly")
    local_nonpersistent_flags+=("--assembly=")
    flags+=("--background=")
    two_word_flags+=("--background")
    local_nonpersistent_flags+=("--background")
    local_nonpersistent_flags+=("--background=")
    flags+=("--cache=")
    two_word_flags+=("--cache")
    local_nonpersistent_flags+=("--cache")
    local_nonpersistent_flags+=("--cache=")
    flags+=("--cache-dir=")
    two_word_flags+=("--cache-dir")
    local_nonpersistent_flags+=("--cache-dir")
    local_nonpersistent_flags+=("--cache-dir=")
    flags+=("--color=")
    two_word_flags+=("--color")
    local_nonpersistent_flags+=("--color")
    local_nonpersistent_flags+=("--color=")
    flags+=("--focus=")
    two_word_flags+=("--focus")
    local_nonpersistent_flags+=("--focus")
    local_nonpersistent_flags+=("--focus=")
    flags+=("--format=")
    two_word_flags+=("--format")
    local_nonpersistent_flags+=("--format")
    local_nonpersistent_flags+=("--format=")
    flags+=("--format-input=")
    two_word_flags+=("--format-input")
    local_nonpersistent_flags+=("--format-input")
    local_nonpersistent_flags+=("--format-input=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--max-atoms=")
    two_word_flags+=("--max-atoms")
    local_nonpersistent_flags+=("--max-atoms")
    local_nonpersistent_flags+=("--max-atoms=")
    flags+=("--max-download-bytes=")
    two_word_flags+=("--max-download-bytes")
    local_nonpersistent_flags+=("--max-download-bytes")
    local_nonpersistent_flags+=("--max-download-bytes=")
    flags+=("--max-outputs=")
    two_word_flags+=("--max-outputs")
    local_nonpersistent_flags+=("--max-outputs")
    local_nonpersistent_flags+=("--max-outputs=")
    flags+=("--max-pixels=")
    two_word_flags+=("--max-pixels")
    local_nonpersistent_flags+=("--max-pixels")
    local_nonpersistent_flags+=("--max-pixels=")
    flags+=("--no-cache")
    local_nonpersistent_flags+=("--no-cache")
    flags+=("--no-network")
    local_nonpersistent_flags+=("--no-network")
    flags+=("--offline")
    local_nonpersistent_flags+=("--offline")
    flags+=("--out=")
    two_word_flags+=("--out")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    local_nonpersistent_flags+=("-o")
    flags+=("--preset=")
    two_word_flags+=("--preset")
    local_nonpersistent_flags+=("--preset")
    local_nonpersistent_flags+=("--preset=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    local_nonpersistent_flags+=("--profile")
    local_nonpersistent_flags+=("--profile=")
    flags+=("--provider=")
    two_word_flags+=("--provider")
    local_nonpersistent_flags+=("--provider")
    local_nonpersistent_flags+=("--provider=")
    flags+=("--repr=")
    two_word_flags+=("--repr")
    local_nonpersistent_flags+=("--repr")
    local_nonpersistent_flags+=("--repr=")
    flags+=("--select=")
    two_word_flags+=("--select")
    local_nonpersistent_flags+=("--select")
    local_nonpersistent_flags+=("--select=")
    flags+=("--size=")
    two_word_flags+=("--size")
    local_nonpersistent_flags+=("--size")
    local_nonpersistent_flags+=("--size=")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout=")
    flags+=("--view=")
    two_word_flags+=("--view")
    local_nonpersistent_flags+=("--view")
    local_nonpersistent_flags+=("--view=")
    flags+=("--write=")
    two_word_flags+=("--write")
    local_nonpersistent_flags+=("--write")
    local_nonpersistent_flags+=("--write=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_job_schema()
{
    last_command="molstar_job_schema"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--info")
    local_nonpersistent_flags+=("--info")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--out=")
    two_word_flags+=("--out")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    local_nonpersistent_flags+=("-o")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_job_validate()
{
    last_command="molstar_job_validate"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--schema")
    local_nonpersistent_flags+=("--schema")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_job()
{
    last_command="molstar_job"

    command_aliases=()

    commands=()
    commands+=("examples")
    commands+=("explain")
    commands+=("init")
    commands+=("migrate")
    commands+=("normalize")
    commands+=("schema")
    commands+=("validate")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()


    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_jobs_prune()
{
    last_command="molstar_jobs_prune"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--dry-run")
    local_nonpersistent_flags+=("--dry-run")
    flags+=("--job-store=")
    two_word_flags+=("--job-store")
    local_nonpersistent_flags+=("--job-store")
    local_nonpersistent_flags+=("--job-store=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--ttl=")
    two_word_flags+=("--ttl")
    local_nonpersistent_flags+=("--ttl")
    local_nonpersistent_flags+=("--ttl=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_jobs()
{
    last_command="molstar_jobs"

    command_aliases=()

    commands=()
    commands+=("prune")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()


    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_logs_export()
{
    last_command="molstar_logs_export"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--dir=")
    two_word_flags+=("--dir")
    local_nonpersistent_flags+=("--dir")
    local_nonpersistent_flags+=("--dir=")
    flags+=("--include-inputs")
    local_nonpersistent_flags+=("--include-inputs")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--max-input-bytes=")
    two_word_flags+=("--max-input-bytes")
    local_nonpersistent_flags+=("--max-input-bytes")
    local_nonpersistent_flags+=("--max-input-bytes=")
    flags+=("--max-single-input-bytes=")
    two_word_flags+=("--max-single-input-bytes")
    local_nonpersistent_flags+=("--max-single-input-bytes")
    local_nonpersistent_flags+=("--max-single-input-bytes=")
    flags+=("--out=")
    two_word_flags+=("--out")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    local_nonpersistent_flags+=("-o")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_logs_import()
{
    last_command="molstar_logs_import"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--dir=")
    two_word_flags+=("--dir")
    local_nonpersistent_flags+=("--dir")
    local_nonpersistent_flags+=("--dir=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_logs_list()
{
    last_command="molstar_logs_list"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--dir=")
    two_word_flags+=("--dir")
    local_nonpersistent_flags+=("--dir")
    local_nonpersistent_flags+=("--dir=")
    flags+=("--failed")
    local_nonpersistent_flags+=("--failed")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--limit=")
    two_word_flags+=("--limit")
    local_nonpersistent_flags+=("--limit")
    local_nonpersistent_flags+=("--limit=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_logs_prune()
{
    last_command="molstar_logs_prune"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--dir=")
    two_word_flags+=("--dir")
    local_nonpersistent_flags+=("--dir")
    local_nonpersistent_flags+=("--dir=")
    flags+=("--dry-run")
    local_nonpersistent_flags+=("--dry-run")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--older-than=")
    two_word_flags+=("--older-than")
    local_nonpersistent_flags+=("--older-than")
    local_nonpersistent_flags+=("--older-than=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_logs_rerun()
{
    last_command="molstar_logs_rerun"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--out-dir=")
    two_word_flags+=("--out-dir")
    local_nonpersistent_flags+=("--out-dir")
    local_nonpersistent_flags+=("--out-dir=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_logs_show()
{
    last_command="molstar_logs_show"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--dir=")
    two_word_flags+=("--dir")
    local_nonpersistent_flags+=("--dir")
    local_nonpersistent_flags+=("--dir=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--last")
    local_nonpersistent_flags+=("--last")
    flags+=("--open-output")
    local_nonpersistent_flags+=("--open-output")
    flags+=("--out-dir=")
    two_word_flags+=("--out-dir")
    local_nonpersistent_flags+=("--out-dir")
    local_nonpersistent_flags+=("--out-dir=")
    flags+=("--rerun")
    local_nonpersistent_flags+=("--rerun")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_logs_verify()
{
    last_command="molstar_logs_verify"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--strict")
    local_nonpersistent_flags+=("--strict")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_logs()
{
    last_command="molstar_logs"

    command_aliases=()

    commands=()
    commands+=("export")
    commands+=("import")
    commands+=("list")
    commands+=("prune")
    commands+=("rerun")
    commands+=("show")
    commands+=("verify")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--dir=")
    two_word_flags+=("--dir")
    local_nonpersistent_flags+=("--dir")
    local_nonpersistent_flags+=("--dir=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--last")
    local_nonpersistent_flags+=("--last")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_presets_list()
{
    last_command="molstar_presets_list"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_presets_show()
{
    last_command="molstar_presets_show"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_presets()
{
    last_command="molstar_presets"

    command_aliases=()

    commands=()
    commands+=("list")
    commands+=("show")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()


    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_quickstart()
{
    last_command="molstar_quickstart"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--open")
    local_nonpersistent_flags+=("--open")
    flags+=("--out=")
    two_word_flags+=("--out")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    flags+=("--out-dir=")
    two_word_flags+=("--out-dir")
    local_nonpersistent_flags+=("--out-dir")
    local_nonpersistent_flags+=("--out-dir=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_recipe_compile()
{
    last_command="molstar_recipe_compile"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--allow-host=")
    two_word_flags+=("--allow-host")
    local_nonpersistent_flags+=("--allow-host")
    local_nonpersistent_flags+=("--allow-host=")
    flags+=("--allow-path=")
    two_word_flags+=("--allow-path")
    local_nonpersistent_flags+=("--allow-path")
    local_nonpersistent_flags+=("--allow-path=")
    flags+=("--cache=")
    two_word_flags+=("--cache")
    local_nonpersistent_flags+=("--cache")
    local_nonpersistent_flags+=("--cache=")
    flags+=("--cache-dir=")
    two_word_flags+=("--cache-dir")
    local_nonpersistent_flags+=("--cache-dir")
    local_nonpersistent_flags+=("--cache-dir=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--max-atoms=")
    two_word_flags+=("--max-atoms")
    local_nonpersistent_flags+=("--max-atoms")
    local_nonpersistent_flags+=("--max-atoms=")
    flags+=("--max-download-bytes=")
    two_word_flags+=("--max-download-bytes")
    local_nonpersistent_flags+=("--max-download-bytes")
    local_nonpersistent_flags+=("--max-download-bytes=")
    flags+=("--max-outputs=")
    two_word_flags+=("--max-outputs")
    local_nonpersistent_flags+=("--max-outputs")
    local_nonpersistent_flags+=("--max-outputs=")
    flags+=("--max-pixels=")
    two_word_flags+=("--max-pixels")
    local_nonpersistent_flags+=("--max-pixels")
    local_nonpersistent_flags+=("--max-pixels=")
    flags+=("--no-cache")
    local_nonpersistent_flags+=("--no-cache")
    flags+=("--no-network")
    local_nonpersistent_flags+=("--no-network")
    flags+=("--offline")
    local_nonpersistent_flags+=("--offline")
    flags+=("--out=")
    two_word_flags+=("--out")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    local_nonpersistent_flags+=("-o")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    local_nonpersistent_flags+=("--profile")
    local_nonpersistent_flags+=("--profile=")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_recipe_explain()
{
    last_command="molstar_recipe_explain"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--schema")
    local_nonpersistent_flags+=("--schema")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_recipe_init()
{
    last_command="molstar_recipe_init"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--format=")
    two_word_flags+=("--format")
    local_nonpersistent_flags+=("--format")
    local_nonpersistent_flags+=("--format=")
    flags+=("--id=")
    two_word_flags+=("--id")
    local_nonpersistent_flags+=("--id")
    local_nonpersistent_flags+=("--id=")
    flags+=("--image-out=")
    two_word_flags+=("--image-out")
    local_nonpersistent_flags+=("--image-out")
    local_nonpersistent_flags+=("--image-out=")
    flags+=("--out=")
    two_word_flags+=("--out")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    local_nonpersistent_flags+=("-o")
    flags+=("--path=")
    two_word_flags+=("--path")
    local_nonpersistent_flags+=("--path")
    local_nonpersistent_flags+=("--path=")
    flags+=("--provider=")
    two_word_flags+=("--provider")
    local_nonpersistent_flags+=("--provider")
    local_nonpersistent_flags+=("--provider=")
    flags+=("--size=")
    two_word_flags+=("--size")
    local_nonpersistent_flags+=("--size")
    local_nonpersistent_flags+=("--size=")
    flags+=("--url=")
    two_word_flags+=("--url")
    local_nonpersistent_flags+=("--url")
    local_nonpersistent_flags+=("--url=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_recipe_schema()
{
    last_command="molstar_recipe_schema"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--info")
    local_nonpersistent_flags+=("--info")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--out=")
    two_word_flags+=("--out")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    local_nonpersistent_flags+=("-o")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_recipe_validate()
{
    last_command="molstar_recipe_validate"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--schema")
    local_nonpersistent_flags+=("--schema")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_recipe()
{
    last_command="molstar_recipe"

    command_aliases=()

    commands=()
    commands+=("compile")
    commands+=("explain")
    commands+=("init")
    commands+=("schema")
    commands+=("validate")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()


    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_render()
{
    last_command="molstar_render"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--allow-host=")
    two_word_flags+=("--allow-host")
    local_nonpersistent_flags+=("--allow-host")
    local_nonpersistent_flags+=("--allow-host=")
    flags+=("--allow-path=")
    two_word_flags+=("--allow-path")
    local_nonpersistent_flags+=("--allow-path")
    local_nonpersistent_flags+=("--allow-path=")
    flags+=("--assembly=")
    two_word_flags+=("--assembly")
    local_nonpersistent_flags+=("--assembly")
    local_nonpersistent_flags+=("--assembly=")
    flags+=("--background=")
    two_word_flags+=("--background")
    local_nonpersistent_flags+=("--background")
    local_nonpersistent_flags+=("--background=")
    flags+=("--cache=")
    two_word_flags+=("--cache")
    local_nonpersistent_flags+=("--cache")
    local_nonpersistent_flags+=("--cache=")
    flags+=("--cache-dir=")
    two_word_flags+=("--cache-dir")
    local_nonpersistent_flags+=("--cache-dir")
    local_nonpersistent_flags+=("--cache-dir=")
    flags+=("--ci-artifact=")
    two_word_flags+=("--ci-artifact")
    local_nonpersistent_flags+=("--ci-artifact")
    local_nonpersistent_flags+=("--ci-artifact=")
    flags+=("--color=")
    two_word_flags+=("--color")
    local_nonpersistent_flags+=("--color")
    local_nonpersistent_flags+=("--color=")
    flags+=("--compact")
    local_nonpersistent_flags+=("--compact")
    flags+=("--demo")
    local_nonpersistent_flags+=("--demo")
    flags+=("--diagnose")
    local_nonpersistent_flags+=("--diagnose")
    flags+=("--dry-run")
    local_nonpersistent_flags+=("--dry-run")
    flags+=("--explain")
    local_nonpersistent_flags+=("--explain")
    flags+=("--focus=")
    two_word_flags+=("--focus")
    local_nonpersistent_flags+=("--focus")
    local_nonpersistent_flags+=("--focus=")
    flags+=("--format=")
    two_word_flags+=("--format")
    local_nonpersistent_flags+=("--format")
    local_nonpersistent_flags+=("--format=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--log-asset-max-bytes=")
    two_word_flags+=("--log-asset-max-bytes")
    local_nonpersistent_flags+=("--log-asset-max-bytes")
    local_nonpersistent_flags+=("--log-asset-max-bytes=")
    flags+=("--log-assets")
    local_nonpersistent_flags+=("--log-assets")
    flags+=("--log-assets-max-bytes=")
    two_word_flags+=("--log-assets-max-bytes")
    local_nonpersistent_flags+=("--log-assets-max-bytes")
    local_nonpersistent_flags+=("--log-assets-max-bytes=")
    flags+=("--max-atoms=")
    two_word_flags+=("--max-atoms")
    local_nonpersistent_flags+=("--max-atoms")
    local_nonpersistent_flags+=("--max-atoms=")
    flags+=("--max-download-bytes=")
    two_word_flags+=("--max-download-bytes")
    local_nonpersistent_flags+=("--max-download-bytes")
    local_nonpersistent_flags+=("--max-download-bytes=")
    flags+=("--max-outputs=")
    two_word_flags+=("--max-outputs")
    local_nonpersistent_flags+=("--max-outputs")
    local_nonpersistent_flags+=("--max-outputs=")
    flags+=("--max-pixels=")
    two_word_flags+=("--max-pixels")
    local_nonpersistent_flags+=("--max-pixels")
    local_nonpersistent_flags+=("--max-pixels=")
    flags+=("--mvs=")
    two_word_flags+=("--mvs")
    local_nonpersistent_flags+=("--mvs")
    local_nonpersistent_flags+=("--mvs=")
    flags+=("--no-cache")
    local_nonpersistent_flags+=("--no-cache")
    flags+=("--no-log")
    local_nonpersistent_flags+=("--no-log")
    flags+=("--no-network")
    local_nonpersistent_flags+=("--no-network")
    flags+=("--offline")
    local_nonpersistent_flags+=("--offline")
    flags+=("--open")
    local_nonpersistent_flags+=("--open")
    flags+=("--out=")
    two_word_flags+=("--out")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    local_nonpersistent_flags+=("-o")
    flags+=("--preset=")
    two_word_flags+=("--preset")
    local_nonpersistent_flags+=("--preset")
    local_nonpersistent_flags+=("--preset=")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    local_nonpersistent_flags+=("--profile")
    local_nonpersistent_flags+=("--profile=")
    flags+=("--provider=")
    two_word_flags+=("--provider")
    local_nonpersistent_flags+=("--provider")
    local_nonpersistent_flags+=("--provider=")
    flags+=("--quiet")
    local_nonpersistent_flags+=("--quiet")
    flags+=("--renderer-command=")
    two_word_flags+=("--renderer-command")
    local_nonpersistent_flags+=("--renderer-command")
    local_nonpersistent_flags+=("--renderer-command=")
    flags+=("--renderer-mode=")
    two_word_flags+=("--renderer-mode")
    local_nonpersistent_flags+=("--renderer-mode")
    local_nonpersistent_flags+=("--renderer-mode=")
    flags+=("--report=")
    two_word_flags+=("--report")
    local_nonpersistent_flags+=("--report")
    local_nonpersistent_flags+=("--report=")
    flags+=("--repr=")
    two_word_flags+=("--repr")
    local_nonpersistent_flags+=("--repr")
    local_nonpersistent_flags+=("--repr=")
    flags+=("--run-label=")
    two_word_flags+=("--run-label")
    local_nonpersistent_flags+=("--run-label")
    local_nonpersistent_flags+=("--run-label=")
    flags+=("--select=")
    two_word_flags+=("--select")
    local_nonpersistent_flags+=("--select")
    local_nonpersistent_flags+=("--select=")
    flags+=("--show-report")
    local_nonpersistent_flags+=("--show-report")
    flags+=("--size=")
    two_word_flags+=("--size")
    local_nonpersistent_flags+=("--size")
    local_nonpersistent_flags+=("--size=")
    flags+=("--state=")
    two_word_flags+=("--state")
    local_nonpersistent_flags+=("--state")
    local_nonpersistent_flags+=("--state=")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout=")
    flags+=("--verbose")
    local_nonpersistent_flags+=("--verbose")
    flags+=("--view=")
    two_word_flags+=("--view")
    local_nonpersistent_flags+=("--view")
    local_nonpersistent_flags+=("--view=")
    flags+=("--worker-command=")
    two_word_flags+=("--worker-command")
    local_nonpersistent_flags+=("--worker-command")
    local_nonpersistent_flags+=("--worker-command=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_rpc_capabilities()
{
    last_command="molstar_rpc_capabilities"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--socket=")
    two_word_flags+=("--socket")
    local_nonpersistent_flags+=("--socket")
    local_nonpersistent_flags+=("--socket=")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout=")
    flags+=("--token=")
    two_word_flags+=("--token")
    local_nonpersistent_flags+=("--token")
    local_nonpersistent_flags+=("--token=")
    flags+=("--url=")
    two_word_flags+=("--url")
    local_nonpersistent_flags+=("--url")
    local_nonpersistent_flags+=("--url=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_rpc_explain()
{
    last_command="molstar_rpc_explain"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--socket=")
    two_word_flags+=("--socket")
    local_nonpersistent_flags+=("--socket")
    local_nonpersistent_flags+=("--socket=")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout=")
    flags+=("--token=")
    two_word_flags+=("--token")
    local_nonpersistent_flags+=("--token")
    local_nonpersistent_flags+=("--token=")
    flags+=("--url=")
    two_word_flags+=("--url")
    local_nonpersistent_flags+=("--url")
    local_nonpersistent_flags+=("--url=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_rpc_metrics()
{
    last_command="molstar_rpc_metrics"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--socket=")
    two_word_flags+=("--socket")
    local_nonpersistent_flags+=("--socket")
    local_nonpersistent_flags+=("--socket=")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout=")
    flags+=("--token=")
    two_word_flags+=("--token")
    local_nonpersistent_flags+=("--token")
    local_nonpersistent_flags+=("--token=")
    flags+=("--url=")
    two_word_flags+=("--url")
    local_nonpersistent_flags+=("--url")
    local_nonpersistent_flags+=("--url=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_rpc_raw()
{
    last_command="molstar_rpc_raw"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--params=")
    two_word_flags+=("--params")
    local_nonpersistent_flags+=("--params")
    local_nonpersistent_flags+=("--params=")
    flags+=("--socket=")
    two_word_flags+=("--socket")
    local_nonpersistent_flags+=("--socket")
    local_nonpersistent_flags+=("--socket=")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout=")
    flags+=("--token=")
    two_word_flags+=("--token")
    local_nonpersistent_flags+=("--token")
    local_nonpersistent_flags+=("--token=")
    flags+=("--url=")
    two_word_flags+=("--url")
    local_nonpersistent_flags+=("--url")
    local_nonpersistent_flags+=("--url=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_rpc_render()
{
    last_command="molstar_rpc_render"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--async")
    local_nonpersistent_flags+=("--async")
    flags+=("--dry-run")
    local_nonpersistent_flags+=("--dry-run")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--socket=")
    two_word_flags+=("--socket")
    local_nonpersistent_flags+=("--socket")
    local_nonpersistent_flags+=("--socket=")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout=")
    flags+=("--token=")
    two_word_flags+=("--token")
    local_nonpersistent_flags+=("--token")
    local_nonpersistent_flags+=("--token=")
    flags+=("--url=")
    two_word_flags+=("--url")
    local_nonpersistent_flags+=("--url")
    local_nonpersistent_flags+=("--url=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_rpc_validate()
{
    last_command="molstar_rpc_validate"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--socket=")
    two_word_flags+=("--socket")
    local_nonpersistent_flags+=("--socket")
    local_nonpersistent_flags+=("--socket=")
    flags+=("--strict")
    local_nonpersistent_flags+=("--strict")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout=")
    flags+=("--token=")
    two_word_flags+=("--token")
    local_nonpersistent_flags+=("--token")
    local_nonpersistent_flags+=("--token=")
    flags+=("--url=")
    two_word_flags+=("--url")
    local_nonpersistent_flags+=("--url")
    local_nonpersistent_flags+=("--url=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_rpc()
{
    last_command="molstar_rpc"

    command_aliases=()

    commands=()
    commands+=("capabilities")
    commands+=("explain")
    commands+=("metrics")
    commands+=("raw")
    commands+=("render")
    commands+=("validate")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()


    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_scene_compile()
{
    last_command="molstar_scene_compile"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--allow-host=")
    two_word_flags+=("--allow-host")
    local_nonpersistent_flags+=("--allow-host")
    local_nonpersistent_flags+=("--allow-host=")
    flags+=("--allow-path=")
    two_word_flags+=("--allow-path")
    local_nonpersistent_flags+=("--allow-path")
    local_nonpersistent_flags+=("--allow-path=")
    flags+=("--cache=")
    two_word_flags+=("--cache")
    local_nonpersistent_flags+=("--cache")
    local_nonpersistent_flags+=("--cache=")
    flags+=("--cache-dir=")
    two_word_flags+=("--cache-dir")
    local_nonpersistent_flags+=("--cache-dir")
    local_nonpersistent_flags+=("--cache-dir=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--max-atoms=")
    two_word_flags+=("--max-atoms")
    local_nonpersistent_flags+=("--max-atoms")
    local_nonpersistent_flags+=("--max-atoms=")
    flags+=("--max-download-bytes=")
    two_word_flags+=("--max-download-bytes")
    local_nonpersistent_flags+=("--max-download-bytes")
    local_nonpersistent_flags+=("--max-download-bytes=")
    flags+=("--max-outputs=")
    two_word_flags+=("--max-outputs")
    local_nonpersistent_flags+=("--max-outputs")
    local_nonpersistent_flags+=("--max-outputs=")
    flags+=("--max-pixels=")
    two_word_flags+=("--max-pixels")
    local_nonpersistent_flags+=("--max-pixels")
    local_nonpersistent_flags+=("--max-pixels=")
    flags+=("--no-cache")
    local_nonpersistent_flags+=("--no-cache")
    flags+=("--no-network")
    local_nonpersistent_flags+=("--no-network")
    flags+=("--offline")
    local_nonpersistent_flags+=("--offline")
    flags+=("--out=")
    two_word_flags+=("--out")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--out")
    local_nonpersistent_flags+=("--out=")
    local_nonpersistent_flags+=("-o")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    local_nonpersistent_flags+=("--profile")
    local_nonpersistent_flags+=("--profile=")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_scene_validate()
{
    last_command="molstar_scene_validate"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--no-extra")
    local_nonpersistent_flags+=("--no-extra")
    flags+=("--validate-command=")
    two_word_flags+=("--validate-command")
    local_nonpersistent_flags+=("--validate-command")
    local_nonpersistent_flags+=("--validate-command=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_scene()
{
    last_command="molstar_scene"

    command_aliases=()

    commands=()
    commands+=("compile")
    commands+=("validate")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()


    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_selectors_explain()
{
    last_command="molstar_selectors_explain"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_selectors_list()
{
    last_command="molstar_selectors_list"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_selectors()
{
    last_command="molstar_selectors"

    command_aliases=()

    commands=()
    commands+=("explain")
    commands+=("list")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()


    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_self-test()
{
    last_command="molstar_self-test"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--keep")
    local_nonpersistent_flags+=("--keep")
    flags+=("--out-dir=")
    two_word_flags+=("--out-dir")
    local_nonpersistent_flags+=("--out-dir")
    local_nonpersistent_flags+=("--out-dir=")
    flags+=("--require-worker")
    local_nonpersistent_flags+=("--require-worker")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout=")
    flags+=("--verbose")
    local_nonpersistent_flags+=("--verbose")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_serve_smoke()
{
    last_command="molstar_serve_smoke"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--probe-interval=")
    two_word_flags+=("--probe-interval")
    local_nonpersistent_flags+=("--probe-interval")
    local_nonpersistent_flags+=("--probe-interval=")
    flags+=("--probe-out-dir=")
    two_word_flags+=("--probe-out-dir")
    local_nonpersistent_flags+=("--probe-out-dir")
    local_nonpersistent_flags+=("--probe-out-dir=")
    flags+=("--probe-timeout=")
    two_word_flags+=("--probe-timeout")
    local_nonpersistent_flags+=("--probe-timeout")
    local_nonpersistent_flags+=("--probe-timeout=")
    flags+=("--render-probe")
    local_nonpersistent_flags+=("--render-probe")
    flags+=("--socket=")
    two_word_flags+=("--socket")
    local_nonpersistent_flags+=("--socket")
    local_nonpersistent_flags+=("--socket=")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout=")
    flags+=("--token=")
    two_word_flags+=("--token")
    local_nonpersistent_flags+=("--token")
    local_nonpersistent_flags+=("--token=")
    flags+=("--url=")
    two_word_flags+=("--url")
    local_nonpersistent_flags+=("--url")
    local_nonpersistent_flags+=("--url=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_serve()
{
    last_command="molstar_serve"

    command_aliases=()

    commands=()
    commands+=("smoke")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--addr=")
    two_word_flags+=("--addr")
    local_nonpersistent_flags+=("--addr")
    local_nonpersistent_flags+=("--addr=")
    flags+=("--allow-host=")
    two_word_flags+=("--allow-host")
    local_nonpersistent_flags+=("--allow-host")
    local_nonpersistent_flags+=("--allow-host=")
    flags+=("--allow-path=")
    two_word_flags+=("--allow-path")
    local_nonpersistent_flags+=("--allow-path")
    local_nonpersistent_flags+=("--allow-path=")
    flags+=("--auth-token=")
    two_word_flags+=("--auth-token")
    local_nonpersistent_flags+=("--auth-token")
    local_nonpersistent_flags+=("--auth-token=")
    flags+=("--cache=")
    two_word_flags+=("--cache")
    local_nonpersistent_flags+=("--cache")
    local_nonpersistent_flags+=("--cache=")
    flags+=("--cache-dir=")
    two_word_flags+=("--cache-dir")
    local_nonpersistent_flags+=("--cache-dir")
    local_nonpersistent_flags+=("--cache-dir=")
    flags+=("--dry-run")
    local_nonpersistent_flags+=("--dry-run")
    flags+=("--foreground-report")
    local_nonpersistent_flags+=("--foreground-report")
    flags+=("--job-store=")
    two_word_flags+=("--job-store")
    local_nonpersistent_flags+=("--job-store")
    local_nonpersistent_flags+=("--job-store=")
    flags+=("--job-ttl=")
    two_word_flags+=("--job-ttl")
    local_nonpersistent_flags+=("--job-ttl")
    local_nonpersistent_flags+=("--job-ttl=")
    flags+=("--max-atoms=")
    two_word_flags+=("--max-atoms")
    local_nonpersistent_flags+=("--max-atoms")
    local_nonpersistent_flags+=("--max-atoms=")
    flags+=("--max-download-bytes=")
    two_word_flags+=("--max-download-bytes")
    local_nonpersistent_flags+=("--max-download-bytes")
    local_nonpersistent_flags+=("--max-download-bytes=")
    flags+=("--max-outputs=")
    two_word_flags+=("--max-outputs")
    local_nonpersistent_flags+=("--max-outputs")
    local_nonpersistent_flags+=("--max-outputs=")
    flags+=("--max-pixels=")
    two_word_flags+=("--max-pixels")
    local_nonpersistent_flags+=("--max-pixels")
    local_nonpersistent_flags+=("--max-pixels=")
    flags+=("--no-cache")
    local_nonpersistent_flags+=("--no-cache")
    flags+=("--no-network")
    local_nonpersistent_flags+=("--no-network")
    flags+=("--no-worker")
    local_nonpersistent_flags+=("--no-worker")
    flags+=("--offline")
    local_nonpersistent_flags+=("--offline")
    flags+=("--openapi")
    local_nonpersistent_flags+=("--openapi")
    flags+=("--prewarm")
    local_nonpersistent_flags+=("--prewarm")
    flags+=("--profile=")
    two_word_flags+=("--profile")
    local_nonpersistent_flags+=("--profile")
    local_nonpersistent_flags+=("--profile=")
    flags+=("--queue=")
    two_word_flags+=("--queue")
    local_nonpersistent_flags+=("--queue")
    local_nonpersistent_flags+=("--queue=")
    flags+=("--quiet")
    local_nonpersistent_flags+=("--quiet")
    flags+=("--renderer-command=")
    two_word_flags+=("--renderer-command")
    local_nonpersistent_flags+=("--renderer-command")
    local_nonpersistent_flags+=("--renderer-command=")
    flags+=("--request-timeout=")
    two_word_flags+=("--request-timeout")
    local_nonpersistent_flags+=("--request-timeout")
    local_nonpersistent_flags+=("--request-timeout=")
    flags+=("--socket=")
    two_word_flags+=("--socket")
    local_nonpersistent_flags+=("--socket")
    local_nonpersistent_flags+=("--socket=")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout=")
    flags+=("--verbose")
    local_nonpersistent_flags+=("--verbose")
    flags+=("--worker-command=")
    two_word_flags+=("--worker-command")
    local_nonpersistent_flags+=("--worker-command")
    local_nonpersistent_flags+=("--worker-command=")
    flags+=("--workers=")
    two_word_flags+=("--workers")
    local_nonpersistent_flags+=("--workers")
    local_nonpersistent_flags+=("--workers=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_server_cancel()
{
    last_command="molstar_server_cancel"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--socket=")
    two_word_flags+=("--socket")
    local_nonpersistent_flags+=("--socket")
    local_nonpersistent_flags+=("--socket=")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout=")
    flags+=("--token=")
    two_word_flags+=("--token")
    local_nonpersistent_flags+=("--token")
    local_nonpersistent_flags+=("--token=")
    flags+=("--url=")
    two_word_flags+=("--url")
    local_nonpersistent_flags+=("--url")
    local_nonpersistent_flags+=("--url=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_server_events()
{
    last_command="molstar_server_events"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--socket=")
    two_word_flags+=("--socket")
    local_nonpersistent_flags+=("--socket")
    local_nonpersistent_flags+=("--socket=")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout=")
    flags+=("--token=")
    two_word_flags+=("--token")
    local_nonpersistent_flags+=("--token")
    local_nonpersistent_flags+=("--token=")
    flags+=("--url=")
    two_word_flags+=("--url")
    local_nonpersistent_flags+=("--url")
    local_nonpersistent_flags+=("--url=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_server_logs()
{
    last_command="molstar_server_logs"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--request-timeout=")
    two_word_flags+=("--request-timeout")
    local_nonpersistent_flags+=("--request-timeout")
    local_nonpersistent_flags+=("--request-timeout=")
    flags+=("--socket=")
    two_word_flags+=("--socket")
    local_nonpersistent_flags+=("--socket")
    local_nonpersistent_flags+=("--socket=")
    flags+=("--token=")
    two_word_flags+=("--token")
    local_nonpersistent_flags+=("--token")
    local_nonpersistent_flags+=("--token=")
    flags+=("--url=")
    two_word_flags+=("--url")
    local_nonpersistent_flags+=("--url")
    local_nonpersistent_flags+=("--url=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_server_status()
{
    last_command="molstar_server_status"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--socket=")
    two_word_flags+=("--socket")
    local_nonpersistent_flags+=("--socket")
    local_nonpersistent_flags+=("--socket=")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout=")
    flags+=("--token=")
    two_word_flags+=("--token")
    local_nonpersistent_flags+=("--token")
    local_nonpersistent_flags+=("--token=")
    flags+=("--url=")
    two_word_flags+=("--url")
    local_nonpersistent_flags+=("--url")
    local_nonpersistent_flags+=("--url=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_server_submit()
{
    last_command="molstar_server_submit"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--download-outputs")
    local_nonpersistent_flags+=("--download-outputs")
    flags+=("--dry-run")
    local_nonpersistent_flags+=("--dry-run")
    flags+=("--interval=")
    two_word_flags+=("--interval")
    local_nonpersistent_flags+=("--interval")
    local_nonpersistent_flags+=("--interval=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--out-dir=")
    two_word_flags+=("--out-dir")
    local_nonpersistent_flags+=("--out-dir")
    local_nonpersistent_flags+=("--out-dir=")
    flags+=("--socket=")
    two_word_flags+=("--socket")
    local_nonpersistent_flags+=("--socket")
    local_nonpersistent_flags+=("--socket=")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout=")
    flags+=("--token=")
    two_word_flags+=("--token")
    local_nonpersistent_flags+=("--token")
    local_nonpersistent_flags+=("--token=")
    flags+=("--url=")
    two_word_flags+=("--url")
    local_nonpersistent_flags+=("--url")
    local_nonpersistent_flags+=("--url=")
    flags+=("--wait")
    local_nonpersistent_flags+=("--wait")
    flags+=("--wait-timeout=")
    two_word_flags+=("--wait-timeout")
    local_nonpersistent_flags+=("--wait-timeout")
    local_nonpersistent_flags+=("--wait-timeout=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_server_wait()
{
    last_command="molstar_server_wait"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--download-outputs")
    local_nonpersistent_flags+=("--download-outputs")
    flags+=("--interval=")
    two_word_flags+=("--interval")
    local_nonpersistent_flags+=("--interval")
    local_nonpersistent_flags+=("--interval=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--out-dir=")
    two_word_flags+=("--out-dir")
    local_nonpersistent_flags+=("--out-dir")
    local_nonpersistent_flags+=("--out-dir=")
    flags+=("--request-timeout=")
    two_word_flags+=("--request-timeout")
    local_nonpersistent_flags+=("--request-timeout")
    local_nonpersistent_flags+=("--request-timeout=")
    flags+=("--socket=")
    two_word_flags+=("--socket")
    local_nonpersistent_flags+=("--socket")
    local_nonpersistent_flags+=("--socket=")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout=")
    flags+=("--token=")
    two_word_flags+=("--token")
    local_nonpersistent_flags+=("--token")
    local_nonpersistent_flags+=("--token=")
    flags+=("--url=")
    two_word_flags+=("--url")
    local_nonpersistent_flags+=("--url")
    local_nonpersistent_flags+=("--url=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_server()
{
    last_command="molstar_server"

    command_aliases=()

    commands=()
    commands+=("cancel")
    commands+=("events")
    commands+=("logs")
    commands+=("status")
    commands+=("submit")
    commands+=("wait")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()


    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_smoke()
{
    last_command="molstar_smoke"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--out-dir=")
    two_word_flags+=("--out-dir")
    local_nonpersistent_flags+=("--out-dir")
    local_nonpersistent_flags+=("--out-dir=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_update-runtime()
{
    last_command="molstar_update-runtime"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    local_nonpersistent_flags+=("--config")
    local_nonpersistent_flags+=("--config=")
    flags+=("--home=")
    two_word_flags+=("--home")
    local_nonpersistent_flags+=("--home")
    local_nonpersistent_flags+=("--home=")
    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--skip-install")
    local_nonpersistent_flags+=("--skip-install")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_version()
{
    last_command="molstar_version"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--json")
    local_nonpersistent_flags+=("--json")
    flags+=("--skip-runtime")
    local_nonpersistent_flags+=("--skip-runtime")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout=")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_molstar_root_command()
{
    last_command="molstar"

    command_aliases=()

    commands=()
    commands+=("agent")
    commands+=("batch")
    commands+=("bench")
    commands+=("cache")
    commands+=("capabilities")
    commands+=("compat")
    commands+=("completion")
    commands+=("diagnose")
    commands+=("docs")
    commands+=("doctor")
    commands+=("examples")
    commands+=("fixtures")
    commands+=("help")
    commands+=("inspect")
    commands+=("install-artifact")
    commands+=("install-local")
    if [[ -z "${BASH_VERSION:-}" || "${BASH_VERSINFO[0]:-}" -gt 3 ]]; then
        command_aliases+=("install")
        aliashash["install"]="install-local"
    fi
    commands+=("job")
    commands+=("jobs")
    commands+=("logs")
    commands+=("presets")
    commands+=("quickstart")
    commands+=("recipe")
    commands+=("render")
    commands+=("rpc")
    commands+=("scene")
    commands+=("selectors")
    commands+=("self-test")
    if [[ -z "${BASH_VERSION:-}" || "${BASH_VERSINFO[0]:-}" -gt 3 ]]; then
        command_aliases+=("selftest")
        aliashash["selftest"]="self-test"
    fi
    commands+=("serve")
    commands+=("server")
    commands+=("smoke")
    commands+=("update-runtime")
    commands+=("version")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()


    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

__start_molstar()
{
    local cur prev words cword split
    declare -A flaghash 2>/dev/null || :
    declare -A aliashash 2>/dev/null || :
    if declare -F _init_completion >/dev/null 2>&1; then
        _init_completion -s || return
    else
        __molstar_init_completion -n "=" || return
    fi

    local c=0
    local flag_parsing_disabled=
    local flags=()
    local two_word_flags=()
    local local_nonpersistent_flags=()
    local flags_with_completion=()
    local flags_completion=()
    local commands=("molstar")
    local command_aliases=()
    local must_have_one_flag=()
    local must_have_one_noun=()
    local has_completion_function=""
    local last_command=""
    local nouns=()
    local noun_aliases=()

    __molstar_handle_word
}

if [[ $(type -t compopt) = "builtin" ]]; then
    complete -o default -F __start_molstar molstar
else
    complete -o default -o nospace -F __start_molstar molstar
fi

# ex: ts=4 sw=4 et filetype=sh
