package main

import (
	"bufio"
	"fmt"
	"os"

	"github.com/lazybark/pcloud-sync-server/cloud/server"
)

func main() {
	/*
		Сервер считает количество подключений для каждого юзера
		Он учитывает, что другая сторона запросила максимум N активных подключений и не будет слать лишний раз, точно так же, как и клиент

		Сервер




	*/

	// Как ускорить работу с массивами?

	//Сервис, на который сервера могут манифестировать свой ип. Для домашнего использования. Например: ты генеришь там токен, и сервер используя его отправляет туда каждый раз свой новый IP, а юзер откуда-то его узнает и использует, чтобы приконнектиться к домашнему серверу. Либо у меня сервачок, который выдает тебе адрес и захлодя на него, ты попадаешь на свой домашний сервер

	//Mode в конфиге решает, в каком режиме будет работать сервер
	// SERVER - regular main server, MIRROR - server that just gets and stores current structure
	// ENDPOINT - server that does not store data - just passes on fs-events

	//File versioning

	//Рутина, которая чистит старый action buffer
	//To DO: -v flag for server verbose logging, -f for filesystem verbose logging
	//-s flag for total silent mode
	//-t for full user+token truncate at start
	//-b for backing up the DB
	//--stats
	//-c use colorful output in console (true by default)

	// add close for all channels in listener!!!

	//Сервер отсылает обратно свой конфиг: макс число одновременных коннектов, макс размер файла
	//Макс соединений не может быть меньше 2 (одно для информации, другое для отправки/приема файлов)

	//TO DO: divide files and folders between users
	//TO DO: encode files on the fly
	// Single client mode - delete all tokens if new registered

	// Duplicate servers to avoid data loss
	// Data archiving and file versioning

	// При старте писать, сколько файлов обрабатывается в файловой системе

	// Client sends DeviceName

	/// QR-код, чтобы поделиться файлом
	// Полноценная возможность делиться файлами из приватного хранилища

	// Полностью консольная версия облака

	// Отдельная настройка, чтобы логировать только в консоль / файл / и туда, и туда

	// Указать, что для любых интов 0 - это выключено

	// Как распространять сертификаты?

	// Сколько было активно соединение
	// Сколько трафика прогнал клиент
	// Подхват конфига на лету
	// Ограничние на максимальный размер файла
	// Ограничение трафика / кол-ва файлов и прочего
	//Требовать перелогин при каждой новой сессии

	// Exclude from sync

	// Возможность конфигурировать сервер как бекапный - только получает данные, ничего не отправляет. Тогда он и не следит за изменениями

	//Собирать ошибки в том числе

	// Что очищать после закрытия сервера?

	// Допилить возобновление скачивания файла в случае обрыва соединения

	// Рутина, котоарая чистит устаревшие соединения

	// Uint vs int

	// Возможность удаленно тормознуть сервер командой. Закрывается через канал serverDone в методе Stop()

	// Программа поиска самых больишх файлов
	// Одинаковых файлов
	// Файлы и папки исключенные из синка или привязанные к устройствам

	// Запуск по расписанию для синхронизации бекапов
	// Бекап-сервер

	server := server.NewServer()
	started := make(chan bool)
	go server.Start(started)

	<-started
	for {
		fmt.Println("Type command to server or 'h' for help:")
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Scan()
		c := scanner.Text()
		if c == "h" || c == "help" {
			fmt.Println(server.PrintHelp())
		} else if c == "v" || c == "ver" || c == "version" {
			fmt.Println(server.PrintStatistics())
		} else if c == "stat" || c == "stats" || c == "statistic" {
			fmt.Println(server.PrintVersion())
		} else if c == "status" {
			fmt.Println(server.PrintStatus())
		}
	}

	//users.CreateUserCLI(sqlite)

}
