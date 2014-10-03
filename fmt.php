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
		list($name, $value) = is_array($t) ? $t : [NULL, $t];

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
			if ($output->getName() === T_COMMENT) {
				$output->setValue(rtrim($output->getValue()));
				$value = PHP_EOL.$value;
			}
			$value = removeEmptyLines($value);

		} elseif ($expectBrace) {
			$expectBrace = FALSE;
			sanitizePreviousWhitespace($output);
			if ($value !== '{') {
				$output->push(NULL, '{');
				$output->push(T_WHITESPACE, PHP_EOL."$indent\t");
				$braceAfterSemicolon = TRUE;
			}

		} elseif ($value === ';') {
			if ($braceAfterSemicolon) {
				$braceAfterSemicolon = FALSE;
				$output->push($name, $value);
				$output->push(T_WHITESPACE, "\n$indent");
				$name = NULL;
				$value = '}';
			}
			$searchingFunction = FALSE;

		} elseif (preg_match('/^true|false|null$/i', $value)) {
			$value = strtoupper($value);

		} elseif (in_array($name, [T_IF, T_ELSEIF, T_ELSE, T_SWITCH, T_WHILE, T_FOR, T_FOREACH, T_TRY, T_CATCH])) {
			$indent = getIndent($output->getValue());
			$writeSpace = TRUE;

			if ($name === T_TRY || $name == T_ELSE) {
				$expectBrace = TRUE;
			} else {
				$catchParenthesis = TRUE;
			}
			in_array($name, [T_CATCH, T_ELSEIF, T_ELSE]) && sanitizePreviousWhitespace($output);

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

function removeEmptyLines($content) {
	return preg_replace('/\n.*\n.*\n/s', "\n".PHP_EOL, $content);
}

function removeTrailingWhitespace($content) {
	return preg_replace('/[ \t]+$/m', '', $content);
}

function ensureTrailingEol($content) {
	return rtrim($content).PHP_EOL;
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
	return preg_replace_callback('/^[ \t]+/m', function ($m) use ($tabWidth) {
		$chars = count_chars($m[0]);
		$tabs = $chars[9];
		$spaces = $chars[32];
		$tabs += floor($spaces/$tabWidth);
		return str_repeat("\t", $tabs).str_repeat(' ', $spaces % $tabWidth);
	}, $content);
}
