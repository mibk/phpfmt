<?php

$file = @$_SERVER['argv'][1];

// 1. normalizovat if/elseif/else/try/catch/while/for/foreach
// 2. odstranit prázdné řádky na 1 prázdný řádek
// 3. true, false, null -- velké
// 4. odstranit trailing whitespace
// 5. trailing newline on EOF

if ($file === NULL) {
	echo "Usage: php fmt.php <filename>\n";
	exit(1);
}

$content = file_get_contents($file);

$content = fmt($content);
$content = convertSpacesToTabs($content);
$content = removeTrailingWhitespace($content);
$content = ensureTrailingEol($content);

file_put_contents($file, $content);

///////// Functions

function fmt($content) {
	$doWhile = FALSE;
	$expectIf = FALSE;
	$braceOnNextLine = FALSE;
	$searchingFunction = FALSE;
	$writeSpace = FALSE;
	$catchParenthesis = FALSE;
	$expectBrace = FALSE;
	$braceAfterSemicolon = FALSE;
	$output = new Output;
	$indent = NULL;

	$tokens = token_get_all($content);
	for ($i = 0; $i < count($tokens); $i++) {
		$t = $tokens[$i];
		list($name, $value) = sanitizeToken($t);

		if ($writeSpace) {
			$writeSpace = FALSE;
			if ($name === T_WHITESPACE) {
				$value = ' ';
			} else {
				$output->push(T_WHITESPACE, ' ');
			}

		}
		if ($catchParenthesis === 0) {
			$catchParenthesis = FALSE;
			$expectBrace = TRUE;
		}

		if ($name === T_WHITESPACE) {
			if (isOneLineComment($output->getName(), $output->getValue())) {
				$output->setValue(rtrim($output->getValue()));
				$value = PHP_EOL.$value;
			}
			$value = removeEmptyLines($value);

		} elseif ($expectBrace) {
			$expectBrace = FALSE;
			if ($expectIf && $name === T_IF) {
				$output->delete();
				$output->delete();
				$name = T_ELSEIF;
				$value = 'elseif';
				$catchParenthesis = TRUE;
				$writeSpace = TRUE;
			} else {
				sanitizePreviousWhitespace($output);
				if ($value !== '{') {
					$output->push(NULL, '{');
					$output->push(T_WHITESPACE, PHP_EOL."$indent\t");

					if ($value === ';') {
						list($name, $value) = finishBraceBlock($output, $indent);
					} else {
						$braceAfterSemicolon = TRUE;
					}
				}
			}

		} elseif (isOneLineComment($name, $value)) {
			$value = preg_replace('#^//\s*#', '// ', $value);

		} elseif ($value === ';') {
			if ($braceAfterSemicolon) {
				$braceAfterSemicolon = FALSE;
				$output->push($name, $value);
				list($name, $value) = finishBraceBlock($output, $indent);
			}
			$searchingFunction = FALSE;

		} elseif (preg_match('/^true|false|null$/i', $value)) {
			$value = strtoupper($value);

		} elseif (in_array($name, [T_IF, T_ELSEIF, T_ELSE, T_SWITCH, T_DO, T_WHILE, T_FOR, T_FOREACH, T_TRY, T_CATCH])) {
			$indent = getIndent($output->getValue());
			$writeSpace = TRUE;

			if ($name === T_DO) {
				$doWhile = TRUE;
				$expectBrace = TRUE;
			} elseif ($name === T_TRY || $name === T_ELSE) {
				$expectBrace = TRUE;
				$name === T_ELSE && $expectIf = TRUE;
			} elseif (!$doWhile) {
				$catchParenthesis = TRUE;
			}
			if (
				$output->getName(1) !== T_COMMENT
				&& (in_array($name, [T_CATCH, T_ELSEIF, T_ELSE]) || $name === T_WHILE && $doWhile)
			) {
				sanitizePreviousWhitespace($output);
			}

		} elseif ($catchParenthesis) {
			if ($value === '(') {
				$catchParenthesis === TRUE && $catchParenthesis = 0;
				$catchParenthesis++;
			} elseif ($value === ')') {
				$catchParenthesis--;
			}

		} elseif (in_array($name, [T_PUBLIC, T_PROTECTED, T_PRIVATE])) {
			$indent = getIndent($output->getLastNewLineWhitespace());
			$searchingFunction = TRUE;

		} elseif ($name === T_CLASS || $name === T_FUNCTION && $searchingFunction) {
			$name === T_CLASS && $indent = getIndent($output->getLastNewLineWhitespace());
			$braceOnNextLine = TRUE;
			$searchingFunction = FALSE;

		} elseif ($value === '{' && $braceOnNextLine) {
			$braceOnNextLine = FALSE;
			sanitizePreviousWhitespace($output, PHP_EOL."$indent");
		}


		$output->push($name, $value);
	}

	return (string) $output;
}

function sanitizeToken($token) {
	return is_array($token) ? $token : [NULL, $token];
}

function getIndent($whitespace) {
	preg_match('/[\t ]*$/', $whitespace, $m);
	return $m[0];
}

function sanitizePreviousWhitespace(Output $output, $value = ' ') {
	if ($output->getName() === T_WHITESPACE) {
		$output->setValue($value);
	} else {
		$output->push(T_WHITESPACE, $value);
	}
}

function finishBraceBlock(Output $output, $indent) {
	$output->push(T_WHITESPACE, PHP_EOL."$indent");
	return [NULL, '}'];
}

function removeEmptyLines($content) {
	return preg_replace('/\n.*\n.*\n/s', "\n".PHP_EOL, $content);
}

function removeTrailingWhitespace($content) {
	return preg_replace('/[ \t]+$/m', '', $content);
}

function ensureTrailingEol($content) {
	return rtrim($content).PHP_EOL;
}

function isOneLineComment($name, $value) {
	return $name === T_COMMENT && strpos($value, '/*') !== 0;
}


class Output
{
	private $array = [];

	public function push($name, $value)
	{
		$this->array[] = [$name, $value];
	}

	public function getName($stepsBack = 0)
	{
		if (($token = $this->get($stepsBack)) === NULL) {
			return NULL;
		}
		return $token[0];
	}

	public function getValue($stepsBack = 0)
	{
		if (($token = $this->get($stepsBack)) === NULL) {
			return NULL;
		}
		return $token[1];
	}

	public function setValue($value, $stepsBack = 0)
	{
		if (($index = $this->getIndex($stepsBack)) === NULL) {
			return;
		}
		$this->array[$index][1] = $value;
	}

	private function get($stepsBack) {
		if (($i = $this->getIndex($stepsBack)) === NULL) {
			return NULL;
		}
		return $this->array[$i];
	}

	private function getIndex($stepsBack) {
		$i = count($this->array) - $stepsBack - 1;
		if ($i < 0) {
			return NULL;
		}
		return $i;
	}

	public function delete($stepsBack = 0) {
		$i = $this->getIndex($stepsBack);
		array_splice($this->array, $i, 1);
	}

	public function getLastNewLineWhitespace()
	{
		foreach (array_reverse($this->array) as $token) {
			if ($token[0] === T_WHITESPACE && strpos($token[1], "\n") !== FALSE) {
				return $token[1];
			}
		}
		return NULL;
	}

	public function __tostring()
	{
		$output = '';
		foreach ($this->array as $token) {
			$output .= $token[1];
		}
		return $output;
	}
}

function convertSpacesToTabs($content, $tabWidth = 4) {
	$tokens = token_get_all($content);

	$values = [];
	$eol = FALSE;
	foreach ($tokens as $token) {
		list($name, $value) = sanitizeToken($token);
		if (isOneLineComment($name, $value)) {
			$value = rtrim($value, "\r\n");
			$eol && $value = PHP_EOL.$value;
			$eol = TRUE;
		} elseif ($name === T_WHITESPACE) {
			if ($eol) {
				$value = PHP_EOL.$value;
				$eol = FALSE;
			}
			$value = preg_replace_callback('/^\n[ \t]+/m', function ($m) use ($tabWidth) {
				$chars = count_chars($m[0]);
				$tabs = $chars[9];
				$spaces = $chars[32];
				$tabs += floor($spaces/$tabWidth);
				return "\n".str_repeat("\t", $tabs).str_repeat(' ', $spaces % $tabWidth);
			}, $value);
		} elseif ($eol) {
			$values[] = PHP_EOL;
			$eol = FALSE;
		}
		$values[] = $value;
	}
	return implode('', $values);
}
